package sourceobservation

import (
	"encoding/json"
	"os"
	"testing"
)

type canonicalJSONFixture struct {
	Profile    string                     `json:"profile"`
	Cases      []canonicalJSONFixtureCase `json:"cases"`
	Rejections []canonicalJSONFixtureCase `json:"rejections"`
}

type canonicalJSONFixtureCase struct {
	Name      string `json:"name"`
	Input     string `json:"input"`
	Canonical string `json:"canonical"`
	SHA256    string `json:"sha256"`
}

func TestCanonicalJSONV1Fixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/canonical_json_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture canonicalJSONFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode canonical JSON fixture: %v", err)
	}
	if fixture.Profile != CanonicalJSONProfileV1 || len(fixture.Cases) == 0 || len(fixture.Rejections) == 0 {
		t.Fatalf("invalid canonical JSON fixture header: profile=%q cases=%d rejections=%d", fixture.Profile, len(fixture.Cases), len(fixture.Rejections))
	}

	for _, testCase := range fixture.Cases {
		t.Run(testCase.Name, func(t *testing.T) {
			assertCanonicalJSONFixtureCase(t, &testCase)
		})
	}

	for _, testCase := range fixture.Rejections {
		t.Run(testCase.Name, func(t *testing.T) {
			if _, err := CanonicalizeJSON([]byte(testCase.Input)); err == nil {
				t.Fatal("fixture input must be rejected")
			}
		})
	}
}

func assertCanonicalJSONFixtureCase(t *testing.T, testCase *canonicalJSONFixtureCase) {
	t.Helper()
	canonical, err := CanonicalizeJSON([]byte(testCase.Input))
	if err != nil {
		t.Fatalf("canonicalize fixture input: %v", err)
	}
	if string(canonical) != testCase.Canonical {
		t.Fatalf("canonical JSON = %q, want %q", canonical, testCase.Canonical)
	}
	if got := SHA256Hex(canonical); got != testCase.SHA256 {
		t.Fatalf("canonical SHA-256 = %s, want %s", got, testCase.SHA256)
	}
	canonicalAgain, err := CanonicalizeJSON(canonical)
	if err != nil {
		t.Fatalf("canonicalize canonical fixture output: %v", err)
	}
	if string(canonicalAgain) != testCase.Canonical {
		t.Fatalf("canonical JSON is not idempotent: %q", canonicalAgain)
	}
}

func TestMarshalPayloadV1RejectsInvalidGoString(t *testing.T) {
	_, err := MarshalPayloadV1(CommunityPayloadV1{ChannelID: string([]byte{0xff})})
	if err == nil {
		t.Fatal("typed payload with invalid UTF-8 must be rejected before json.Marshal replacement")
	}
}
