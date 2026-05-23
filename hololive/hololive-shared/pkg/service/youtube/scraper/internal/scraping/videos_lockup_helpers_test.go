package scraping

import (
	"testing"

	"github.com/tidwall/gjson"
)

func TestCollectLockupTexts_SkipsEmptyEntries(t *testing.T) {
	t.Parallel()

	parts := gjson.Parse(`[
		{"text":{"content":"3.2K views"}},
		{"text":{"content":""}},
		{"text":{"content":"2 hours ago"}}
	]`)

	got := collectLockupTexts(parts)

	want := []string{"3.2K views", "2 hours ago"}
	if len(got) != len(want) {
		t.Fatalf("len want %d, got %d (%v)", len(want), len(got), got)
	}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("[%d] want %q, got %q", i, v, got[i])
		}
	}
}

func TestCollectLockupTexts_HandlesEmptyArray(t *testing.T) {
	t.Parallel()

	parts := gjson.Parse(`[]`)

	if got := collectLockupTexts(parts); len(got) != 0 {
		t.Fatalf("want empty slice, got %v", got)
	}
}

func TestPickViewCountAndPublished_FindsViewCountAtAnyIndex(t *testing.T) {
	t.Parallel()

	texts := []string{"2 hours ago", "3.2K views"}
	viewCount, published, ok := pickViewCountAndPublished(texts)

	if !ok {
		t.Fatalf("ok want true, got false")
	}
	if viewCount != 3200 {
		t.Fatalf("viewCount want 3200, got %d", viewCount)
	}
	if published != "2 hours ago" {
		t.Fatalf("published want %q, got %q", "2 hours ago", published)
	}
}

func TestPickViewCountAndPublished_ReturnsFalseWhenNoViewCount(t *testing.T) {
	t.Parallel()

	texts := []string{"2 hours ago", "Premiered"}
	_, _, ok := pickViewCountAndPublished(texts)

	if ok {
		t.Fatalf("ok want false, got true")
	}
}

func TestPickViewCountAndPublished_EmptyPublishedWhenSingleEntry(t *testing.T) {
	t.Parallel()

	texts := []string{"3.2K views"}
	viewCount, published, ok := pickViewCountAndPublished(texts)

	if !ok {
		t.Fatalf("ok want true, got false")
	}
	if viewCount != 3200 {
		t.Fatalf("viewCount want 3200, got %d", viewCount)
	}
	if published != "" {
		t.Fatalf("published want empty, got %q", published)
	}
}

func TestFallbackPickMetadata_UsesFirstTwoTexts(t *testing.T) {
	t.Parallel()

	viewCount, published := fallbackPickMetadata([]string{"3.2K views", "Premiered"})

	if viewCount != 3200 {
		t.Fatalf("viewCount want 3200, got %d", viewCount)
	}
	if published != "Premiered" {
		t.Fatalf("published want %q, got %q", "Premiered", published)
	}
}

func TestFallbackPickMetadata_HandlesEmpty(t *testing.T) {
	t.Parallel()

	viewCount, published := fallbackPickMetadata(nil)

	if viewCount != 0 {
		t.Fatalf("viewCount want 0, got %d", viewCount)
	}
	if published != "" {
		t.Fatalf("published want empty, got %q", published)
	}
}

func TestPickLockupMetadataTexts_PrefersViewCountFromAnyPosition(t *testing.T) {
	t.Parallel()

	parts := gjson.Parse(`[
		{"text":{"content":"2 hours ago"}},
		{"text":{"content":"3.2K views"}}
	]`)

	viewCount, published := pickLockupMetadataTexts(parts)

	if viewCount != 3200 {
		t.Fatalf("viewCount want 3200, got %d", viewCount)
	}
	if published != "2 hours ago" {
		t.Fatalf("published want %q, got %q", "2 hours ago", published)
	}
}

func TestPickLockupMetadataTexts_FallbackWhenNoViewCount(t *testing.T) {
	t.Parallel()

	parts := gjson.Parse(`[
		{"text":{"content":"Premiered"}},
		{"text":{"content":"5 days ago"}}
	]`)

	viewCount, published := pickLockupMetadataTexts(parts)

	if viewCount != 0 {
		t.Fatalf("viewCount want 0, got %d", viewCount)
	}
	if published != "5 days ago" {
		t.Fatalf("published want %q, got %q", "5 days ago", published)
	}
}
