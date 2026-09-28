package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const garbageInput = "garbage"

func TestParseThumbnailSources(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		thumbs := ParseThumbnailSources(parseGJSONResultPtr(`[{"url":"u1","width":120,"height":90},{"url":"u2","width":480,"height":360}]`))
		require.Len(t, thumbs, 2)
		assert.Equal(t, Thumbnail{URL: "u1", Width: 120, Height: 90}, thumbs[0])
		assert.Equal(t, Thumbnail{URL: "u2", Width: 480, Height: 360}, thumbs[1])
	})

	t.Run("empty array", func(t *testing.T) {
		thumbs := ParseThumbnailSources(parseGJSONResultPtr(`[]`))
		assert.Empty(t, thumbs)
		assert.NotNil(t, thumbs)
	})

	t.Run("non-existent source returns empty", func(t *testing.T) {
		thumbs := ParseThumbnailSources(&gjson.Result{})
		assert.Empty(t, thumbs)
	})

	t.Run("null literal yields empty slice", func(t *testing.T) {
		thumbs := ParseThumbnailSources(parseGJSONResultPtr(`null`))
		assert.Empty(t, thumbs)
		assert.NotNil(t, thumbs)
	})

	t.Run("scalar yields empty slice", func(t *testing.T) {
		thumbs := ParseThumbnailSources(parseGJSONResultPtr(`42`))
		assert.Empty(t, thumbs)
		assert.NotNil(t, thumbs)
	})
}

func TestParseShortNumber(t *testing.T) {
	tests := []struct {
		text string
		want int64
	}{
		{"1.5K", 1500},
		{"2.76M subscribers", 0},
		{"5B", 5_000_000_000},
		{"1,234", 1234},
		{"2.3", 2},
		{"No", 0},
		{"", 0},
		{garbageInput, 0},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseShortNumber(tt.text))
		})
	}
}

func TestParseViewCount(t *testing.T) {
	tests := []struct {
		name string
		text string
		want int64
	}{
		{"english full number", "1,056,229,686 views", 1_056_229_686},
		{"english K suffix", "1.5K views", 1500},
		{"english M suffix bare", "2.76M", 2_760_000},
		{"japanese man", "69万回視聴", 690_000},
		{"korean man", "조회수 1.2만회", 12_000},
		{"korean cheon", "조회수 5천회", 5_000},
		{"korean plain count", "1,234회", 1234},
		{"singular view", "10 view", 10},
		{"no views", "No views", 0},
		{"empty", "", 0},
		{garbageInput, garbageInput, 0},
		{"korean eok", "3.4억", 340_000_000},
		{"korean jo", "조회수 1.5조회", 1_500_000_000_000},
		{"korean jo bare", "2조", 2_000_000_000_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseViewCount(tt.text))
		})
	}
}

func TestParseVideoCount(t *testing.T) {
	tests := []struct {
		text string
		want int64
	}{
		{"2,429 videos", 2429},
		{"1 video", 1},
		{"42", 42},
		{garbageInput, 0},
		{"", 0},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseVideoCount(tt.text))
		})
	}
}
