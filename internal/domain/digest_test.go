package domain

import (
	"encoding/json"
	"testing"
)

func TestCanonicalDigest_StableAcrossKeyOrder(t *testing.T) {
	a := []byte(`{"kind":"Profile","apiVersion":"omca.dev/v1alpha1","metadata":{"id":"company:example","extra":"x"}}`)
	b := []byte(`{"metadata":{"extra":"x","id":"company:example"},"apiVersion":"omca.dev/v1alpha1","kind":"Profile"}`)

	var da, db any
	if err := json.Unmarshal(a, &da); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &db); err != nil {
		t.Fatal(err)
	}

	digestA, err := CanonicalDigest(da)
	if err != nil {
		t.Fatal(err)
	}
	digestB, err := CanonicalDigest(db)
	if err != nil {
		t.Fatal(err)
	}

	if digestA != digestB {
		t.Fatalf("digest differs across key order: %s != %s", digestA, digestB)
	}
}

func TestCanonicalDigest_DiffersOnContentChange(t *testing.T) {
	a := map[string]any{"id": "company:example"}
	b := map[string]any{"id": "company:other"}

	digestA, err := CanonicalDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	digestB, err := CanonicalDigest(b)
	if err != nil {
		t.Fatal(err)
	}

	if digestA == digestB {
		t.Fatal("expected different digests for different content")
	}
}

func TestCanonicalDigest_ArrayOrderMatters(t *testing.T) {
	a := map[string]any{"items": []string{"a", "b"}}
	b := map[string]any{"items": []string{"b", "a"}}

	digestA, err := CanonicalDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	digestB, err := CanonicalDigest(b)
	if err != nil {
		t.Fatal(err)
	}

	if digestA == digestB {
		t.Fatal("expected different digests when array order differs")
	}
}
