package sampledata

import (
	"slices"
	"testing"
)

func TestTemplateKeyListAndSetAgree(t *testing.T) {
	t.Parallel()

	keys := GetAllTemplateKeys()

	if len(keys) != len(allTemplateKeySet) {
		t.Fatalf("GetAllTemplateKeys() len = %d, allTemplateKeySet len = %d (duplicate entry in the list?)", len(keys), len(allTemplateKeySet))
	}

	for _, key := range keys {
		if !IsValidTemplateKey(key) {
			t.Errorf("IsValidTemplateKey(%q) = false, want true for a listed key", key)
		}
	}

	for key := range allTemplateKeySet {
		if !slices.Contains(keys, key) {
			t.Errorf("allTemplateKeySet contains %q but GetAllTemplateKeys() does not", key)
		}
	}

	if IsValidTemplateKey("definitely.not.a.template.key") {
		t.Error("IsValidTemplateKey() = true for an unknown key, want false")
	}
}

func TestGetAllTemplateKeysReturnsIndependentSlice(t *testing.T) {
	t.Parallel()

	first := GetAllTemplateKeys()
	if len(first) == 0 {
		t.Fatal("GetAllTemplateKeys() returned no keys")
	}
	original := first[0]
	first[0] = "mutated.by.caller"

	refreshed := GetAllTemplateKeys()
	if len(refreshed) == 0 {
		t.Fatal("GetAllTemplateKeys() returned no keys after caller mutation")
	}
	if got := refreshed[0]; got != original {
		t.Fatalf("caller mutation leaked into the package list: got %q, want %q", got, original)
	}
	if !IsValidTemplateKey(original) {
		t.Fatalf("IsValidTemplateKey(%q) = false after caller mutation", original)
	}
}
