package sourceobservation

import (
	"bytes"
	"encoding/json/jsontext"
	"errors"
	"strings"
	"testing"
)

func TestPayloadJSONBTextSizeCanonicalProfile(t *testing.T) {
	for _, tc := range []struct{ raw, stored string }{
		{`{}`, `{}`},
		{`[]`, `[]`},
		{`[{},[],[null,true,false],{"b":[],"a":{}}]`, `[{}, [], [null, true, false], {"a": {}, "b": []}]`},
		{`[9007199254740991,-9007199254740991,-0,1e3,10.00]`, `[9007199254740991, -9007199254740991, 0, 1000, 10]`},
		{`{"한글😀":"日本語,:\"\\\b\f\n\r\t\u001f/","escaped\"key":":"}`, `{"escaped\"key": ":", "한글😀": "日本語,:\"\\\b\f\n\r\t\u001f/"}`},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			canonical, err := CanonicalizeJSON([]byte(tc.raw))
			if err != nil {
				t.Fatal(err)
			}

			if got := canonicalPayloadJSONBTextSize(canonical); got != len(tc.stored) {
				t.Fatalf("storage size=%d, want %d", got, len(tc.stored))
			}

			payload, err := MarshalPayloadV1(jsontext.Value(tc.raw))
			if err != nil || !bytes.Equal(payload, canonical) || SHA256Hex(payload) != SHA256Hex(canonical) {
				t.Fatalf("accepted canonical bytes/digest changed: %v", err)
			}
		})
	}
}

func TestPayloadStorageExactBoundary(t *testing.T) {
	// ':' 한 개만 공백을 추가한다. 문자열 안 punctuation/escape는 크기 확장을 만들지 않는다.
	for _, extra := range []int{0, 1} {
		raw := jsontext.Value(`{"text":"` + strings.Repeat("x", MaxPayloadBytes-len(`{"text": ""}`)+extra) + `"}`)

		canonical, err := CanonicalizeJSON(raw)
		if err != nil {
			t.Fatal(err)
		}

		if len(canonical) > MaxPayloadBytes {
			t.Fatal("fixture exceeded canonical limit")
		}

		payload, err := MarshalPayloadV1(raw)
		if extra == 0 {
			if err != nil || !bytes.Equal(payload, canonical) {
				t.Fatalf("exact boundary rejected: %v", err)
			}
		} else if !errors.Is(err, ErrPayloadTooLarge) {
			t.Fatalf("one-over error=%v", err)
		}
	}
}

func TestPayloadSizeCauseDoesNotChangeGlobalCanonicalization(t *testing.T) {
	for _, raw := range []jsontext.Value{
		jsontext.Value(`"` + strings.Repeat("x", MaxPayloadBytes) + `"`),
		jsontext.Value(`[` + strings.Repeat(`1e15,`, 70000) + `0]`),
	} {
		if _, err := MarshalPayloadV1(raw); !errors.Is(err, ErrPayloadTooLarge) {
			t.Fatalf("payload size cause=%v", err)
		}

		if _, err := CanonicalizeJSON(raw); err == nil || errors.Is(err, ErrPayloadTooLarge) {
			t.Fatalf("global canonicalization acquired payload classification: %v", err)
		}
	}

	if _, err := MarshalPayloadV1(jsontext.Value(`{"bad":1.5}`)); err == nil || errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("non-size validation hidden: %v", err)
	}
}
