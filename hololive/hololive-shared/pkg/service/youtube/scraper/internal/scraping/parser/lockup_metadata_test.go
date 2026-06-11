package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tidwall/gjson"
)

func TestPickLockupMetadataTexts_ViewCountAndPublished(t *testing.T) {
	parts := gjson.Parse(`[{"text":{"content":"69万回視聴"}},{"text":{"content":"1 month ago"}}]`)
	viewCount, published := PickLockupMetadataTexts(parts)
	assert.Equal(t, int64(690000), viewCount)
	assert.Equal(t, "1 month ago", published)
}

func TestPickLockupMetadataTexts_NoParsableViewCountFallsBack(t *testing.T) {
	parts := gjson.Parse(`[{"text":{"content":"Streamed live"}},{"text":{"content":"2 hours ago"}}]`)
	viewCount, published := PickLockupMetadataTexts(parts)
	assert.Equal(t, int64(0), viewCount)
	assert.Equal(t, "2 hours ago", published)
}

func TestPickLockupMetadataTexts_EmptyInput(t *testing.T) {
	viewCount, published := PickLockupMetadataTexts(gjson.Parse(`[]`))
	assert.Equal(t, int64(0), viewCount)
	assert.Empty(t, published)
}

func TestCollectLockupTexts_SkipsEmpty(t *testing.T) {
	parts := gjson.Parse(`[{"text":{"content":"a"}},{"text":{"content":""}},{"text":{"content":"b"}}]`)
	texts := CollectLockupTexts(parts)
	assert.Equal(t, []string{"a", "b"}, texts)
}

func TestCollectLockupTexts_GarbageInput(t *testing.T) {
	texts := CollectLockupTexts(gjson.Parse(`"not an array"`))
	assert.Empty(t, texts)
}

func TestCollectLockupTexts_EmptyInput(t *testing.T) {
	texts := CollectLockupTexts(gjson.Parse(`[]`))
	assert.Empty(t, texts)
}

func TestPickViewCountAndPublished(t *testing.T) {
	tests := []struct {
		name      string
		texts     []string
		wantCount int64
		wantPub   string
		wantOK    bool
	}{
		{"view first then published", []string{"69万回視聴", "1 month ago"}, 690000, "1 month ago", true},
		{"published first then view", []string{"1 month ago", "69万回視聴"}, 690000, "1 month ago", true},
		{"no parsable view count", []string{"No views", "Streamed 2 hours ago"}, 0, "", false},
		{"all garbage", []string{"garbage", "more garbage"}, 0, "", false},
		{"empty slice", nil, 0, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, pub, ok := PickViewCountAndPublished(tt.texts)
			assert.Equal(t, tt.wantCount, count)
			assert.Equal(t, tt.wantPub, pub)
			assert.Equal(t, tt.wantOK, ok)
		})
	}
}

func TestFallbackPickMetadata(t *testing.T) {
	tests := []struct {
		name      string
		texts     []string
		wantCount int64
		wantPub   string
	}{
		{"two elements", []string{"1.2K views", "2 days ago"}, 1200, "2 days ago"},
		{"one element", []string{"only one"}, 0, ""},
		{"empty slice", nil, 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, pub := FallbackPickMetadata(tt.texts)
			assert.Equal(t, tt.wantCount, count)
			assert.Equal(t, tt.wantPub, pub)
		})
	}
}
