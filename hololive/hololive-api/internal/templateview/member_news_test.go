package templateview

import (
	"log/slog"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	membernewscontracts "github.com/kapu/hololive-shared/pkg/contracts/membernews"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

func TestBuildMemberNewsDigestLocalizesWithoutMutatingInput(t *testing.T) {
	t.Parallel()

	store := messagestrings.NewStore(dbtest.NewPool(t), slog.New(slog.DiscardHandler))
	require.NoError(t, store.Load(t.Context()))

	digest := membernewscontracts.Digest{
		Headline: "소식", MoreSummary: "추가 소식", TotalCount: 99,
		TopItems: []membernewscontracts.SummaryItem{
			{Category: "birthday_live", Title: "A", SourceURL: "https://example.com/a"},
			{Category: "solo_live", Title: "B"},
			{Category: "event", Title: "C"},
			{Category: "collab"},
			{Category: " goods "},
			{Category: "other"},
			{Category: "custom"},
			{Category: ""},
		},
	}
	original := slices.Clone(digest.TopItems)
	data := BuildMemberNewsDigest(digest, store)

	for i, want := range []string{"생일 라이브", "솔로 라이브", "이벤트", "콜라보", "굿즈", "기타", "custom", ""} {
		assert.Equal(t, want, data.TopItems[i].Category)
	}

	assert.Equal(t, original, digest.TopItems)
	assert.Equal(t, digest.Headline, data.Headline)
	assert.Equal(t, digest.MoreSummary, data.MoreSummary)
	assert.Equal(t, 99, data.TotalCount)
	assert.Equal(t, "https://example.com/a", data.TopItems[0].SourceURL)

	data.TopItems[0].Title = "변경"
	assert.Equal(t, original, digest.TopItems)
}

func TestBuildMemberNewsDigestCountsDisplayedBlocks(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items int
		more  string
		want  int
		fold  bool
	}{
		{name: "empty"},
		{name: "one", items: 1, want: 1},
		{name: "two", items: 2, want: 2, fold: true},
		{name: "one with summary", items: 1, more: "추가 소식\n둘째 줄", want: 2, fold: true},
		{name: "summary only", more: "추가 소식", want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := BuildMemberNewsDigest(membernewscontracts.Digest{TopItems: make([]membernewscontracts.SummaryItem, tc.items), MoreSummary: tc.more, TotalCount: 100}, nil)
			assert.Equal(t, tc.want, data.DisplayCount)
			assert.Equal(t, tc.fold, ShouldFoldItems(data.DisplayCount))
		})
	}
}
