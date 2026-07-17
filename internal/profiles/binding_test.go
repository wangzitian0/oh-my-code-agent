package profiles

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

const bindingYAML = `
apiVersion: omca.dev/v1alpha1
kind: Binding
metadata:
  id: binding:order-service
spec:
  match:
    repository: github.com/example/order-service
    paths: ["**"]
  profiles:
    - personal:alice
    - company:example
    - team:payments
    - project:order-service
`

// TestLoadBindings_RealFileOnDisk loads the exact Binding document
// docs/product/requirements.md §4.2 shows, from a real file on disk.
func TestLoadBindings_RealFileOnDisk(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "order-service.yaml"), bindingYAML)

	got, err := LoadBindings([]string{dir})
	if err != nil {
		t.Fatalf("LoadBindings: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("LoadBindings = %d entries, want 1", len(got))
	}
	b := got[0]
	if b.Spec.Match.Repository != "github.com/example/order-service" {
		t.Errorf("repository = %q", b.Spec.Match.Repository)
	}
	if want := []string{"personal:alice", "company:example", "team:payments", "project:order-service"}; !reflect.DeepEqual(b.Spec.Profiles, want) {
		t.Errorf("profiles = %v, want %v", b.Spec.Profiles, want)
	}
}

// goldenBinding builds a Binding in memory (not from disk — MatchBindings
// itself is pure and disk-loading is already covered by
// TestLoadBindings_RealFileOnDisk) for the golden-context matching table
// below.
func goldenBinding(id, repository string, paths, profileIDs []string) domain.Binding {
	return domain.Binding{
		APIVersion: domain.SupportedAPIVersion,
		Kind:       "Binding",
		Metadata:   domain.Metadata{ID: id},
		Spec: domain.BindingSpec{
			Match:    domain.BindingMatch{Repository: repository, Paths: paths},
			Profiles: profileIDs,
		},
	}
}

// TestMatchBindings_GoldenContexts is issue #16 AC #3: "Binding match by
// repository and paths proven with golden contexts." Each case names a
// realistic repository+path context and the Binding(s) that should or
// should not match it.
func TestMatchBindings_GoldenContexts(t *testing.T) {
	wholeRepo := goldenBinding("binding:whole-repo", "github.com/example/order-service", []string{"**"}, []string{"company:example"})
	unscopedRepo := goldenBinding("binding:unscoped", "github.com/example/unscoped", nil, []string{"company:unscoped"})
	monorepoAPI := goldenBinding("binding:monorepo-api", "github.com/example/monorepo", []string{"apps/api/**"}, []string{"team:api"})
	monorepoWeb := goldenBinding("binding:monorepo-web", "github.com/example/monorepo", []string{"apps/web/**"}, []string{"team:web"})
	otherRepo := goldenBinding("binding:other", "github.com/example/other-repo", []string{"**"}, []string{"company:other"})

	all := []domain.Binding{wholeRepo, unscopedRepo, monorepoAPI, monorepoWeb, otherRepo}

	cases := []struct {
		name       string
		repository string
		relPath    string
		want       []string // binding metadata IDs expected to match
	}{
		{
			name:       "whole-repo binding matches the repository root",
			repository: "github.com/example/order-service",
			relPath:    "",
			want:       []string{"binding:whole-repo"},
		},
		{
			name:       "whole-repo binding matches a nested path too",
			repository: "github.com/example/order-service",
			relPath:    "internal/service",
			want:       []string{"binding:whole-repo"},
		},
		{
			name:       "different repository never matches",
			repository: "github.com/example/does-not-exist",
			relPath:    "",
			want:       nil,
		},
		{
			name:       "unscoped paths (nil) matches every path, same as **",
			repository: "github.com/example/unscoped",
			relPath:    "any/deep/path",
			want:       []string{"binding:unscoped"},
		},
		{
			name:       "monorepo: path under apps/api matches only the api binding",
			repository: "github.com/example/monorepo",
			relPath:    "apps/api/handlers/order.go",
			want:       []string{"binding:monorepo-api"},
		},
		{
			name:       "monorepo: path under apps/web matches only the web binding",
			repository: "github.com/example/monorepo",
			relPath:    "apps/web/index.tsx",
			want:       []string{"binding:monorepo-web"},
		},
		{
			name:       "monorepo: path outside both scoped subtrees matches neither",
			repository: "github.com/example/monorepo",
			relPath:    "apps/mobile/App.tsx",
			want:       nil,
		},
		{
			name:       "monorepo: the apps/api directory itself matches (** matches zero further segments)",
			repository: "github.com/example/monorepo",
			relPath:    "apps/api",
			want:       []string{"binding:monorepo-api"},
		},
		{
			name:       "same-named repository but a binding scoped elsewhere never leaks across repositories",
			repository: "github.com/example/other-repo",
			relPath:    "apps/api/handlers/order.go",
			want:       []string{"binding:other"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MatchBindings(all, c.repository, c.relPath)
			gotIDs := make([]string, 0, len(got))
			for _, b := range got {
				gotIDs = append(gotIDs, b.Metadata.ID)
			}
			if !reflect.DeepEqual(gotIDs, c.want) && !(len(gotIDs) == 0 && len(c.want) == 0) {
				t.Errorf("MatchBindings(%q, %q) = %v, want %v", c.repository, c.relPath, gotIDs, c.want)
			}
		})
	}
}

// TestMatchedProfileIDs_UnionAndDedup proves the union of several matched
// Bindings' profiles is deduplicated but keeps first-seen order.
func TestMatchedProfileIDs_UnionAndDedup(t *testing.T) {
	b1 := goldenBinding("b1", "repo", nil, []string{"personal:alice", "company:example"})
	b2 := goldenBinding("b2", "repo", nil, []string{"company:example", "team:payments"})

	got := MatchedProfileIDs([]domain.Binding{b1, b2})
	want := []string{"personal:alice", "company:example", "team:payments"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MatchedProfileIDs = %v, want %v", got, want)
	}
}

// TestGlobMatch_PatternGrammar unit-tests the small doublestar-style
// matcher directly, beyond the golden-context table above.
func TestGlobMatch_PatternGrammar(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"**", "", true},
		{"**", "a/b/c", true},
		{"apps/*/main.go", "apps/api/main.go", true},
		{"apps/*/main.go", "apps/api/sub/main.go", false},
		{"apps/api/**", "apps/api", true},
		{"apps/api/**", "apps/api/x", true},
		{"apps/api/**", "apps/apiextra", false},
		{"apps/web/**", "apps/api/main.go", false},
	}
	for _, c := range cases {
		if got := globMatch(c.pattern, c.path); got != c.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}
