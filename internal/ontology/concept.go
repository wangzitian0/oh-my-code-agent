package ontology

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
)

// LogicalIdentity is the rule that decides whether two physical
// representations of a concept are the same logical entity
// (docs/ontology/README.md §3.2, Resolution contract; each concept file's
// x-logicalIdentity key).
type LogicalIdentity struct {
	Fields []string `json:"fields"`
	Rule   string   `json:"rule"`
}

// ConceptSchema is one loaded ontology concept declaration: a stable
// concept ID, its canonical fields, its logical-identity rule, and the
// merge operators §3.1 allows for it (docs/ontology/README.md §1.1, §3.1).
//
// Named ConceptSchema rather than "Concept" because the package-level
// lookup function below is named Concept, and Go does not allow a type and
// a function to share one identifier in the same package.
type ConceptSchema struct {
	ID              string          `json:"conceptId"`
	Title           string          `json:"title"`
	Description     string          `json:"description"`
	Required        []string        `json:"required"`
	CanonicalFields []string        `json:"-"`
	NonEquivalence  string          `json:"x-nonEquivalenceRule"`
	LogicalIdentity LogicalIdentity `json:"x-logicalIdentity"`
	MergeOperators  []MergeOperator `json:"x-mergeOperators"`
}

// conceptFile mirrors the on-disk JSON shape (ontology/concepts/*.json) for
// decoding; CanonicalFields is derived from Properties rather than stored
// twice.
type conceptFile struct {
	ConceptID       string                     `json:"conceptId"`
	Title           string                     `json:"title"`
	Description     string                     `json:"description"`
	Required        []string                   `json:"required"`
	Properties      map[string]json.RawMessage `json:"properties"`
	NonEquivalence  string                     `json:"x-nonEquivalenceRule"`
	LogicalIdentity LogicalIdentity            `json:"x-logicalIdentity"`
	MergeOperators  []MergeOperator            `json:"x-mergeOperators"`
}

func parseConcept(raw []byte) (ConceptSchema, error) {
	var f conceptFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return ConceptSchema{}, fmt.Errorf("decode concept: %w", err)
	}
	if f.ConceptID == "" {
		return ConceptSchema{}, fmt.Errorf("concept file missing conceptId")
	}
	if len(f.LogicalIdentity.Fields) == 0 {
		return ConceptSchema{}, fmt.Errorf("concept %q missing x-logicalIdentity.fields", f.ConceptID)
	}
	if f.LogicalIdentity.Rule == "" {
		return ConceptSchema{}, fmt.Errorf("concept %q missing x-logicalIdentity.rule", f.ConceptID)
	}
	if f.NonEquivalence == "" {
		return ConceptSchema{}, fmt.Errorf("concept %q missing x-nonEquivalenceRule", f.ConceptID)
	}
	if len(f.MergeOperators) == 0 {
		return ConceptSchema{}, fmt.Errorf("concept %q declares no x-mergeOperators", f.ConceptID)
	}
	for _, op := range f.MergeOperators {
		if err := ValidateMergeOperator(op); err != nil {
			return ConceptSchema{}, fmt.Errorf("concept %q: %w", f.ConceptID, err)
		}
	}

	fields := make([]string, 0, len(f.Properties))
	for name := range f.Properties {
		fields = append(fields, name)
	}
	sort.Strings(fields)

	return ConceptSchema{
		ID:              f.ConceptID,
		Title:           f.Title,
		Description:     f.Description,
		Required:        f.Required,
		CanonicalFields: fields,
		NonEquivalence:  f.NonEquivalence,
		LogicalIdentity: f.LogicalIdentity,
		MergeOperators:  f.MergeOperators,
	}, nil
}

// Registry is a loaded set of ontology concept declarations.
type Registry struct {
	concepts map[string]ConceptSchema
}

// LoadRegistry reads every *.json file directly inside dir as a concept
// declaration (docs/architecture/README.md §6, ontology/concepts/).
func LoadRegistry(dir string) (*Registry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("ontology: read concepts dir %s: %w", dir, err)
	}
	reg := &Registry{concepts: map[string]ConceptSchema{}}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("ontology: read %s: %w", path, err)
		}
		c, err := parseConcept(raw)
		if err != nil {
			return nil, fmt.Errorf("ontology: %s: %w", path, err)
		}
		reg.concepts[c.ID] = c
	}
	return reg, nil
}

// Concept looks up a loaded concept by its stable ID (e.g. "skill") within
// this registry.
func (r *Registry) Concept(id string) (ConceptSchema, bool) {
	c, ok := r.concepts[id]
	return c, ok
}

// IDs returns every concept ID currently loaded, sorted.
func (r *Registry) IDs() []string {
	ids := make([]string, 0, len(r.concepts))
	for id := range r.concepts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

var (
	defaultOnce     sync.Once
	defaultRegistry *Registry
	defaultErr      error
)

// defaultConceptsDir locates ontology/concepts/ relative to this source
// file's own location (via runtime.Caller), so it resolves correctly
// regardless of the caller's working directory or which package imports
// ontology.
func defaultConceptsDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "ontology", "concepts")
}

func loadDefault() (*Registry, error) {
	defaultOnce.Do(func() {
		defaultRegistry, defaultErr = LoadRegistry(defaultConceptsDir())
	})
	return defaultRegistry, defaultErr
}

// Concept looks up a loaded concept by its stable ID (e.g. "skill"),
// loading the default ontology/concepts/ registry on first use. This is the
// lookup normalize.go (and later normalizer PRs) should call instead of
// hard-coding concept facts (docs/architecture/README.md §6: ontology owns
// "schema loading and canonical validation"). Callers that need an
// explicit directory (tests, an alternate concept set) should use
// LoadRegistry and (*Registry).Concept directly instead.
func Concept(id string) (ConceptSchema, bool) {
	reg, err := loadDefault()
	if err != nil {
		return ConceptSchema{}, false
	}
	return reg.Concept(id)
}
