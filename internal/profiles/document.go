package profiles

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// decodeYAMLDocument reads the YAML document at path and decodes it into v,
// a JSON-tagged domain struct (domain.Profile, domain.Binding, domain.
// Activation, domain.Exception). It deliberately does not call
// yaml.Unmarshal(data, v) directly: gopkg.in/yaml.v3 has its own field-name
// convention (lowercased Go field names, or an explicit `yaml:"..."` tag)
// and does not read the `json:"..."` tags every domain type is authored
// with — a direct yaml.Unmarshal into a domain.Profile would silently
// decode every field to its zero value instead of failing loudly, since
// yaml.v3 would look for a field named "apiversion" (no such Go field
// exists verbatim) rather than "APIVersion" tagged `json:"apiVersion"`.
//
// Routing through a generic map first — yaml.Unmarshal into `any`, then
// json.Marshal that generic value, then json.Unmarshal into the typed
// struct — makes the same JSON-tagged struct correctly decode both YAML and
// JSON documents, matching the round-trip idiom this project already uses
// for exactly this "make a generic value behave like its JSON-tagged
// struct" problem (domain.CanonicalDigest, redact.Value).
func decodeYAMLDocument(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("profiles: %s: %w", path, err)
	}
	var generic any
	if err := yaml.Unmarshal(raw, &generic); err != nil {
		return fmt.Errorf("profiles: %s: invalid YAML: %w", path, err)
	}
	jsonBytes, err := json.Marshal(generic)
	if err != nil {
		return fmt.Errorf("profiles: %s: %w", path, err)
	}
	if err := json.Unmarshal(jsonBytes, v); err != nil {
		return fmt.Errorf("profiles: %s: %w", path, err)
	}
	return nil
}

// discoverYAMLFiles returns every *.yaml/*.yml file under root, walked
// recursively and sorted for a deterministic load order. A root that does
// not exist is not an error and yields no files: most of the directories in
// docs/architecture/README.md §7's layout (e.g.
// ~/.config/omca/profiles/task/, <repository>/.omca/exceptions/) are
// optional and commonly absent on a given machine or repository.
func discoverYAMLFiles(root string) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("profiles: %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("profiles: %s: not a directory", root)
	}

	var files []string
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".yaml", ".yml":
			files = append(files, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("profiles: walking %s: %w", root, walkErr)
	}
	sort.Strings(files)
	return files, nil
}
