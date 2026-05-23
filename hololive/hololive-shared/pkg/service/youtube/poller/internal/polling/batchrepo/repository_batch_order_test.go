package batchrepo

import (
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBuildCommunityShortsAlarmStatesReturnsSortedRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	rows := buildCommunityShortsAlarmStates([]*domain.YouTubeContentAlarmTracking{
		{
			Kind:       domain.OutboxKindNewShort,
			ContentID:  "video-b",
			ChannelID:  "channel-b",
			DetectedAt: now.Add(2 * time.Minute),
		},
		{
			Kind:       domain.OutboxKindCommunityPost,
			ContentID:  "post-c",
			ChannelID:  "channel-c",
			DetectedAt: now.Add(time.Minute),
		},
		{
			Kind:       domain.OutboxKindNewShort,
			ContentID:  "video-a",
			ChannelID:  "channel-a",
			DetectedAt: now,
		},
	})

	require.Len(t, rows, 3)

	actualKeys := make([]string, 0, len(rows))
	for _, row := range rows {
		actualKeys = append(actualKeys, string(row.Kind)+"\x00"+row.PostID)
	}

	sortedKeys := append([]string(nil), actualKeys...)
	sort.Strings(sortedKeys)
	require.Equal(t, sortedKeys, actualKeys)
	require.Equal(t, []string{
		string(domain.OutboxKindCommunityPost) + "\x00" + normalizeContentID(domain.OutboxKindCommunityPost, "post-c"),
		string(domain.OutboxKindNewShort) + "\x00" + normalizeContentID(domain.OutboxKindNewShort, "video-a"),
		string(domain.OutboxKindNewShort) + "\x00" + normalizeContentID(domain.OutboxKindNewShort, "video-b"),
	}, actualKeys)
}

func TestCollectShortIdentityAliasesReturnsSortedSlices(t *testing.T) {
	t.Parallel()

	canonicalIDs, aliases := collectShortIdentityAliases(
		[]*domain.YouTubeNotificationOutbox{
			{Kind: domain.OutboxKindNewShort, ContentID: "video-b"},
			{Kind: domain.OutboxKindNewShort, ContentID: "video-a"},
			{Kind: domain.OutboxKindCommunityPost, ContentID: "post-c"},
		},
		[]*domain.YouTubeContentAlarmTracking{
			{Kind: domain.OutboxKindCommunityPost, ContentID: "post-a"},
			{Kind: domain.OutboxKindNewShort, ContentID: "video-a"},
			{Kind: domain.OutboxKindCommunityPost, ContentID: "post-c"},
		},
	)

	require.Equal(t, append([]string(nil), canonicalIDs...), sortedCopy(canonicalIDs))
	require.Equal(t, append([]string(nil), aliases...), sortedCopy(aliases))
	require.Equal(t, []string{
		normalizeContentID(domain.OutboxKindNewShort, "video-a"),
		normalizeContentID(domain.OutboxKindNewShort, "video-b"),
	}, canonicalIDs)
	require.Equal(t, []string{
		normalizeContentID(domain.OutboxKindNewShort, "video-a"),
		normalizeContentID(domain.OutboxKindNewShort, "video-b"),
		normalizeShortVideoResourceID("video-a"),
		normalizeShortVideoResourceID("video-b"),
	}, aliases)
}

func TestNotificationChunksByKindDeduplicatesSameKindContentID(t *testing.T) {
	t.Parallel()

	notifications := []*domain.YouTubeNotificationOutbox{
		{
			Kind:      domain.OutboxKindNewVideo,
			ContentID: "video-1",
			Payload:   `{"kind":"first"}`,
		},
		{
			Kind:      domain.OutboxKindNewShort,
			ContentID: "video-1",
			Payload:   `{"kind":"short"}`,
		},
		{
			Kind:      domain.OutboxKindNewVideo,
			ContentID: "video-1",
			Payload:   `{"kind":"duplicate"}`,
		},
	}

	chunks := notificationChunksByKind(notifications)

	require.Len(t, chunks, 2)
	require.Len(t, chunks[0], 1)
	require.Equal(t, domain.OutboxKindNewVideo, chunks[0][0].Kind)
	require.Equal(t, `{"kind":"first"}`, chunks[0][0].Payload)
	require.Len(t, chunks[1], 1)
	require.Equal(t, domain.OutboxKindNewShort, chunks[1][0].Kind)
}

func sortedCopy(values []string) []string {
	cloned := append([]string(nil), values...)
	sort.Strings(cloned)
	return cloned
}
