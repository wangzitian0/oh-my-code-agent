package domain

import (
	"testing"
	"time"
)

func TestExceptionValid(t *testing.T) {
	valid := Exception{AssetID: "release-production", Scope: "company:example", ExpiresAt: time.Now()}
	if !valid.Valid() {
		t.Error("Exception with AssetID and Scope set should be Valid()")
	}

	missingAsset := Exception{Scope: "company:example"}
	if missingAsset.Valid() {
		t.Error("Exception without AssetID should not be Valid()")
	}

	missingScope := Exception{AssetID: "release-production"}
	if missingScope.Valid() {
		t.Error("Exception without Scope should not be Valid()")
	}
}
