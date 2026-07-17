package profiles

import (
	"fmt"

	"github.com/wangzitian0/oh-my-code-agent/internal/domain"
)

// LoadProfiles reads and validates every Profile YAML document under each of
// dirs (docs/architecture/README.md §7:
// ~/.config/omca/profiles/{personal,company,team,task}/ and
// <repository>/.omca/profiles/). A directory that does not exist is
// skipped, not an error — a fresh machine or repository commonly has some
// of these directories absent (e.g. no company/ profiles yet).
//
// Loading fails closed on the first invalid document: the returned error
// names the offending file path and, via the wrapped domain.ValidateProfile
// error, the specific field that failed (issue #16 AC: "invalid documents
// produce actionable errors naming file and field").
func LoadProfiles(dirs []string) ([]domain.Profile, error) {
	var out []domain.Profile
	for _, dir := range dirs {
		files, err := discoverYAMLFiles(dir)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			var p domain.Profile
			if err := decodeYAMLDocument(f, &p); err != nil {
				return nil, err
			}
			if err := domain.ValidateProfile(p); err != nil {
				return nil, fmt.Errorf("profiles: %s: %w", f, err)
			}
			out = append(out, p)
		}
	}
	return out, nil
}
