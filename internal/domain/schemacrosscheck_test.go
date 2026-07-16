package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// loadSchemaDefEnum reads one $defs.<name>.enum string array out of a JSON
// Schema file on disk. It does not run a general JSON Schema validator (see
// schemas/protocol/common.v1alpha1.schema.json's top comment) — it only
// extracts the closed value list so a Go test can cross-check it.
func loadSchemaDefEnum(t *testing.T, path, defName string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema %s: %v", path, err)
	}
	var doc struct {
		Defs map[string]struct {
			Enum []string `json:"enum"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode schema %s: %v", path, err)
	}
	def, ok := doc.Defs[defName]
	if !ok {
		t.Fatalf("schema %s has no $defs.%s", path, defName)
	}
	out := make(map[string]bool, len(def.Enum))
	for _, v := range def.Enum {
		out[v] = true
	}
	return out
}

// TestHostIDEnum_ConsistentAcrossSchemasAndDomain proves that
// schemas/domain/common's hostId enum, schemas/protocol/common's hostId
// enum (necessarily re-declared rather than $ref'd, since nothing here
// executes a cross-file JSON Schema resolver — see that file's $comment),
// and internal/domain.KnownHostIDs all name the exact same canonical host
// registry (docs/ontology/README.md §4). If any one of the three drifts,
// this test fails rather than three sources of truth silently disagreeing.
func TestHostIDEnum_ConsistentAcrossSchemasAndDomain(t *testing.T) {
	root := filepath.Join("..", "..")
	domainSchema := loadSchemaDefEnum(t, filepath.Join(root, "schemas", "domain", "common.v1alpha1.schema.json"), "hostId")
	protocolSchema := loadSchemaDefEnum(t, filepath.Join(root, "schemas", "protocol", "common.v1alpha1.schema.json"), "hostId")

	if len(domainSchema) == 0 {
		t.Fatal("schemas/domain/common.v1alpha1.schema.json hostId enum is empty")
	}
	if len(domainSchema) != len(protocolSchema) {
		t.Fatalf("hostId enum length differs: domain=%d protocol=%d", len(domainSchema), len(protocolSchema))
	}
	for id := range domainSchema {
		if !protocolSchema[id] {
			t.Errorf("schemas/protocol hostId enum is missing %q, present in schemas/domain", id)
		}
		if !KnownHostIDs[id] {
			t.Errorf("internal/domain.KnownHostIDs is missing %q, present in schemas/domain hostId enum", id)
		}
	}
	for id := range KnownHostIDs {
		if !domainSchema[id] {
			t.Errorf("schemas/domain hostId enum is missing %q, present in internal/domain.KnownHostIDs", id)
		}
	}
}
