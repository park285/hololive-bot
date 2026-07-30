package scraping

import (
	"testing"
	"time"
)

func TestNormalizeSnapshotPayload_SetsCapturedAtWhenZero(t *testing.T) {
	t.Parallel()

	policy := SnapshotPolicy{}
	snapshot := Snapshot{Body: []byte("payload")}

	before := time.Now().UTC()
	got := normalizeSnapshotPayload(&snapshot, policy)
	after := time.Now().UTC()

	if got.CapturedAt.IsZero() {
		t.Fatalf("CapturedAt should be set when zero, got zero value")
	}
	if got.CapturedAt.Before(before) || got.CapturedAt.After(after) {
		t.Fatalf("CapturedAt %v outside expected window [%v, %v]", got.CapturedAt, before, after)
	}
}

func TestNormalizeSnapshotPayload_PreservesNonZeroCapturedAt(t *testing.T) {
	t.Parallel()

	captured := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	snapshot := Snapshot{Body: []byte("payload"), CapturedAt: captured}

	got := normalizeSnapshotPayload(&snapshot, SnapshotPolicy{})

	if !got.CapturedAt.Equal(captured) {
		t.Fatalf("CapturedAt mutated: want %v, got %v", captured, got.CapturedAt)
	}
}

func TestNormalizeSnapshotPayload_SetsSchemaVersionWhenEmpty(t *testing.T) {
	t.Parallel()

	got := normalizeSnapshotPayload(&Snapshot{Body: []byte("p")}, SnapshotPolicy{})

	if got.SchemaVersion != SnapshotSchemaVersion {
		t.Fatalf("SchemaVersion want %q, got %q", SnapshotSchemaVersion, got.SchemaVersion)
	}
}

func TestNormalizeSnapshotPayload_PreservesExistingSchemaVersion(t *testing.T) {
	t.Parallel()

	snapshot := Snapshot{Body: []byte("p"), SchemaVersion: "custom-v2"}

	got := normalizeSnapshotPayload(&snapshot, SnapshotPolicy{})

	if got.SchemaVersion != "custom-v2" {
		t.Fatalf("SchemaVersion mutated: want %q, got %q", "custom-v2", got.SchemaVersion)
	}
}

func TestNormalizeSnapshotPayload_TruncatesBodyToMaxBytes(t *testing.T) {
	t.Parallel()

	policy := SnapshotPolicy{MaxBodyBytes: 4}
	snapshot := Snapshot{Body: []byte("0123456789")}

	got := normalizeSnapshotPayload(&snapshot, policy)

	if string(got.Body) != "0123" {
		t.Fatalf("Body want %q, got %q", "0123", string(got.Body))
	}
}

func TestNormalizeSnapshotPayload_DoesNotTruncateWhenWithinLimit(t *testing.T) {
	t.Parallel()

	policy := SnapshotPolicy{MaxBodyBytes: 32}
	body := []byte("short")
	snapshot := Snapshot{Body: body}

	got := normalizeSnapshotPayload(&snapshot, policy)

	if string(got.Body) != "short" {
		t.Fatalf("Body mutated: want %q, got %q", "short", string(got.Body))
	}
}

func TestNormalizeSnapshotPayload_DoesNotTruncateWhenLimitZero(t *testing.T) {
	t.Parallel()

	policy := SnapshotPolicy{MaxBodyBytes: 0}
	body := []byte("0123456789")
	snapshot := Snapshot{Body: body}

	got := normalizeSnapshotPayload(&snapshot, policy)

	if string(got.Body) != "0123456789" {
		t.Fatalf("Body mutated: want %q, got %q", "0123456789", string(got.Body))
	}
}
