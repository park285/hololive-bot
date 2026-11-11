package timestamp

import (
	"testing"
	"time"
)

func TestNormalizeFormatAndParsePublishedAt(t *testing.T) {
	raw := time.Date(2026, time.April, 10, 10, 11, 12, 123000000, time.FixedZone("KST", 9*60*60))
	normalized := Normalize(raw)

	if normalized.Location() != time.UTC {
		t.Fatalf("Normalize(raw).Location() = %v, want UTC", normalized.Location())
	}

	if got, want := Format(raw), "2026-04-10T01:11:12.123Z"; got != want {
		t.Fatalf("Format(raw) = %q, want %q", got, want)
	}

	parsed, ok := ParsePublishedAt(`"2026-04-10T10:11:12+09:00"`)
	if !ok || parsed == nil {
		t.Fatal("ParsePublishedAt returned no timestamp")
	}

	if got, want := parsed.Format(time.RFC3339Nano), "2026-04-10T01:11:12Z"; got != want {
		t.Fatalf("parsed timestamp = %q, want %q", got, want)
	}
}
