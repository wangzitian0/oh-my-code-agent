package plugin

import "testing"

func TestHostSelectorValidate(t *testing.T) {
	cases := []struct {
		name    string
		sel     HostSelector
		wantErr bool
	}{
		{"valid", HostSelector{HostID: "codex", Surfaces: []string{"cli"}, VersionRange: ">=0.144.0"}, false},
		{"unknown host id", HostSelector{HostID: "not-a-host", Surfaces: []string{"cli"}, VersionRange: ">=1.0.0"}, true},
		{"no surfaces", HostSelector{HostID: "codex", VersionRange: ">=1.0.0"}, true},
		{"empty surface entry", HostSelector{HostID: "codex", Surfaces: []string{""}, VersionRange: ">=1.0.0"}, true},
		{"no version range", HostSelector{HostID: "codex", Surfaces: []string{"cli"}}, true},
	}
	for _, c := range cases {
		err := c.sel.Validate()
		if (err != nil) != c.wantErr {
			t.Errorf("%s: HostSelector.Validate() error = %v, wantErr %v", c.name, err, c.wantErr)
		}
	}
}

func TestPluginManifestValidate(t *testing.T) {
	base := func() PluginManifest {
		return PluginManifest{
			AdapterID:       "codex",
			AdapterVersion:  "0.1.0",
			ContractVersion: ContractVersion,
			Hosts: []HostSelector{
				{HostID: "codex", Surfaces: []string{"cli"}, VersionRange: ">=0.144.0"},
			},
			KnowledgePacks: []KnowledgeRef{{ID: "codex:cli:0.144"}},
			Fixtures:       []FixtureRef{{Path: "fixtures/codex/0.144/user-skill-discovery"}},
		}
	}

	if err := base().Validate(); err != nil {
		t.Fatalf("valid manifest: unexpected error: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*PluginManifest)
	}{
		{"missing adapter id", func(m *PluginManifest) { m.AdapterID = "" }},
		{"missing adapter version", func(m *PluginManifest) { m.AdapterVersion = "" }},
		{"missing contract version", func(m *PluginManifest) { m.ContractVersion = "" }},
		{"no hosts", func(m *PluginManifest) { m.Hosts = nil }},
		{"invalid host selector", func(m *PluginManifest) { m.Hosts[0].HostID = "not-a-host" }},
		{"duplicate host id", func(m *PluginManifest) {
			m.Hosts = append(m.Hosts, HostSelector{HostID: "codex", Surfaces: []string{"editor"}, VersionRange: ">=0.144.0"})
		}},
		{"empty knowledge ref", func(m *PluginManifest) { m.KnowledgePacks = []KnowledgeRef{{}} }},
		{"empty fixture ref", func(m *PluginManifest) { m.Fixtures = []FixtureRef{{}} }},
	}
	for _, c := range cases {
		m := base()
		c.mutate(&m)
		if err := m.Validate(); err == nil {
			t.Errorf("%s: Validate() = nil, want error", c.name)
		}
	}
}

func TestCompatibleContractVersion(t *testing.T) {
	cases := []struct {
		name      string
		expected  string
		candidate string
		wantErr   bool
	}{
		{"exact match", "v1", "v1", false},
		{"same major, different minor", "v1", "v1.4", false},
		{"same major, expected has minor", "v1.0", "v1.9", false},
		{"different major", "v1", "v2", true},
		{"expected missing v prefix", "1", "v1", true},
		{"candidate missing v prefix", "v1", "1", true},
		{"candidate non-numeric major", "v1", "vX", true},
	}
	for _, c := range cases {
		err := CompatibleContractVersion(c.expected, c.candidate)
		if (err != nil) != c.wantErr {
			t.Errorf("%s: CompatibleContractVersion(%q, %q) error = %v, wantErr %v", c.name, c.expected, c.candidate, err, c.wantErr)
		}
	}
}
