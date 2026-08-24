package sourceobservation

import (
	"strings"
	"testing"
)

func BenchmarkCanonicalizeJSON1MiB(b *testing.B) {
	n := MaxPayloadBytes - 8
	raw := []byte(`{"v":"` + strings.Repeat("a", n) + `"}`)

	if len(raw) > MaxPayloadBytes {
		raw = raw[:MaxPayloadBytes]
	}

	b.ReportAllocs()

	for b.Loop() {
		if _, err := CanonicalizeJSON(raw); err != nil {
			b.Fatal(err)
		}
	}
}
