package sourceobservation

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"os"
	"strings"
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

	if err := jsonv2.Unmarshal(raw, &fixture); err != nil {
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
		t.Fatal("typed payload with invalid UTF-8 must be rejected before jsonv2.Marshal replacement")
	}
}

// 출력 버퍼 안에서 member 구간을 재배치하는 정규화가 여러 중첩 단계에서 겹쳐도
// 정확한 바이트를 만들고 호출자 입력을 변경하지 않는지 확인한다.
func TestCanonicalizeJSONOrdersMembersAcrossNestedLevels(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "nested reorders inside arrays and escaped names",
			input: `{"z":{"y":[{"d":1,"c":{"f":"\u0041","e":-0}}],"x":"\n"},"\u0061":[3e0,{"\"q":true,"!":null}]}`,
			want:  `{"a":[3,{"!":null,"\"q":true}],"z":{"x":"\n","y":[{"c":{"e":0,"f":"A"},"d":1}]}}`,
		},
		{
			name:  "prefix names sort before longer names",
			input: `{"ab":1,"a":2,"":3}`,
			want:  `{"":3,"a":2,"ab":1}`,
		},
		{
			name:  "supplementary sorts by surrogate units against BMP",
			input: `{"\uffff":1,"\ud83d\ude01":2,"\ud83d\ude00":3,"\ud7ff":4}`,
			want:  "{\"\ud7ff\":4,\"\U0001F600\":3,\"\U0001F601\":2,\"\uffff\":1}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := []byte(tt.input)
			original := bytes.Clone(raw)

			canonical, err := CanonicalizeJSON(raw)
			if err != nil {
				t.Fatalf("canonicalize: %v", err)
			}

			if string(canonical) != tt.want {
				t.Fatalf("canonical JSON = %s, want %s", canonical, tt.want)
			}

			if !bytes.Equal(raw, original) {
				t.Fatalf("input mutated to %q", raw)
			}
		})
	}
}

func TestCanonicalJSONDepthBoundary(t *testing.T) {
	nested := func(depth int) []byte {
		return []byte(strings.Repeat(`{"a":[`, depth/2) + strings.Repeat("[", depth%2) + "0" +
			strings.Repeat("]", depth%2) + strings.Repeat("]}", depth/2))
	}

	if _, err := CanonicalizeJSON(nested(MaxCanonicalJSONDepth)); err != nil {
		t.Fatalf("canonicalize depth %d: %v", MaxCanonicalJSONDepth, err)
	}

	if _, err := CanonicalizeJSON(nested(MaxCanonicalJSONDepth + 1)); err == nil {
		t.Fatalf("canonicalize depth %d must be rejected", MaxCanonicalJSONDepth+1)
	}

	var decoded any

	if err := decodeStrictJSON(nested(MaxCanonicalJSONDepth), &decoded); err != nil {
		t.Fatalf("strict decode depth %d: %v", MaxCanonicalJSONDepth, err)
	}

	if err := decodeStrictJSON(nested(MaxCanonicalJSONDepth+1), &decoded); err == nil {
		t.Fatalf("strict decode depth %d must be rejected", MaxCanonicalJSONDepth+1)
	}
}

func TestDecodeStrictJSONRejectsNonSingleOrInvalidValues(t *testing.T) {
	tests := map[string]string{
		"empty":            ``,
		"trailing value":   `{} {}`,
		"trailing scalar":  `1 2`,
		"trailing garbage": `{"a":1} x`,
		"duplicate name":   `{"a":1,"\u0061":2}`,
		"lone surrogate":   `{"a":"\ud800"}`,
		"invalid utf8":     "{\"a\":\"\xff\"}",
		"unterminated":     `{"a":[1,2}`,
		"non string name":  `{1:2}`,
		"nested duplicate": `[{"b":{"c":1,"c":1}}]`,
		"extra closing":    `[[[]]]]`,
	}

	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			var decoded any

			if err := decodeStrictJSON([]byte(raw), &decoded); err == nil {
				t.Fatalf("strict decode of %q must be rejected", raw)
			}
		})
	}
}
