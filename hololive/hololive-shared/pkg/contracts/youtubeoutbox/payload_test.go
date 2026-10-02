package youtubeoutbox

import (
	jsonv2 "encoding/json/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// 아래 JSON은 계약 타입을 도입하기 전 domain 구조체를 그대로 직렬화하던 producer의 출력이다. 저장된 payload를
// 읽는 소비자가 있으므로 필드 이름·순서·omitempty가 달라지면 안 된다.
const (
	wantVideoJSON = `{"video_id":"vid00000001","channel_id":"UCabc","title":"새 영상 \"제목\" <b>",` +
		`"published_at":"2026-09-01T12:30:00Z","is_short":false,"is_live_replay":false,"view_count":0,` +
		`"first_seen_at":"0001-01-01T00:00:00Z","last_seen_at":"0001-01-01T00:00:00Z",` +
		`"scheduled_start_at":"2026-09-02T09:00:00Z","is_premiere":true}`
	wantShortJSON = `{"video_id":"shrt0000002","channel_id":"UCabc","title":"short full",` +
		`"thumbnail":[{"url":"https://example.com/t.jpg","width":120,"height":90}],"duration":"0:59",` +
		`"published_text":"2 days ago","published_at":"2026-09-01T12:30:00Z","is_short":true,"is_live_replay":false,` +
		`"view_count":42,"first_seen_at":"2026-09-03T01:02:03.000004Z","last_seen_at":"2026-09-03T01:02:03.000004Z",` +
		`"canonical_post_id":"short:shrt0000002"}`
	wantCommunityJSON = `{"post_id":"UgkxPOST1","channel_id":"UCabc","author_name":"author",` +
		`"author_photo":[{"url":"https://example.com/p.jpg","width":1,"height":1}],"content_text":"본문 & <tag>",` +
		`"published_text":"1 hour ago","published_at":"2026-09-01T12:30:00Z","like_count":3,"comment_count":0,` +
		`"images":[{"url":"https://example.com/a.jpg","width":10,"height":20}],"attached_video":"vidattach01",` +
		`"first_seen_at":"2026-09-03T01:02:03.000004Z","last_seen_at":"2026-09-03T01:02:03.000004Z",` +
		`"canonical_post_id":"community:UgkxPOST1"}`
)

func TestPayloadsKeepStoredJSONShape(t *testing.T) {
	t.Parallel()

	published := time.Date(2026, time.September, 1, 12, 30, 0, 0, time.UTC)
	scheduled := time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC)
	seen := time.Date(2026, time.September, 3, 1, 2, 3, 4000, time.UTC)

	video := Video{
		VideoFields: NewVideoFields(&domain.YouTubeVideo{
			VideoID: "vid00000001", ChannelID: "UCabc", Title: "새 영상 \"제목\" <b>", PublishedAt: &published,
		}),
		ScheduledStartAt: &scheduled,
		IsPremiere:       new(true),
	}
	short := Short{
		VideoFields: NewVideoFields(&domain.YouTubeVideo{
			VideoID: "shrt0000002", ChannelID: "UCabc", Title: "short full", Duration: "0:59",
			PublishedText: "2 days ago", PublishedAt: &published, IsShort: true, ViewCount: 42,
			FirstSeenAt: seen, LastSeenAt: seen,
			Thumbnail: domain.ThumbnailsJSON{{URL: "https://example.com/t.jpg", Width: 120, Height: 90}},
		}),
		CanonicalPostID: "short:shrt0000002",
	}
	community := NewCommunity(&domain.YouTubeCommunityPost{
		PostID: "UgkxPOST1", ChannelID: "UCabc", AuthorName: "author", ContentText: "본문 & <tag>",
		PublishedText: "1 hour ago", PublishedAt: &published, LikeCount: 3,
		Images:        domain.ThumbnailsJSON{{URL: "https://example.com/a.jpg", Width: 10, Height: 20}},
		AuthorPhoto:   domain.ThumbnailsJSON{{URL: "https://example.com/p.jpg", Width: 1, Height: 1}},
		AttachedVideo: "vidattach01", FirstSeenAt: seen, LastSeenAt: seen,
	}, "community:UgkxPOST1")

	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{name: "video", value: video, want: wantVideoJSON},
		{name: "short", value: short, want: wantShortJSON},
		{name: "community", value: community, want: wantCommunityJSON},
	} {
		got, err := jsonv2.Marshal(tc.value)
		require.NoError(t, err, tc.name)
		require.JSONEq(t, tc.want, string(got), tc.name)
		require.Equal(t, tc.want, string(got), tc.name)
	}
}

// 소비자는 저장된 payload를 같은 타입으로 읽으므로, 읽은 값을 다시 직렬화하면 원래 JSON과 같아야 한다.
func TestStoredPayloadsDecodeIntoContractTypes(t *testing.T) {
	t.Parallel()

	var video Video

	require.NoError(t, jsonv2.Unmarshal([]byte(wantVideoJSON), &video))
	require.NotNil(t, video.IsPremiere)
	require.True(t, *video.IsPremiere)
	require.Equal(t, time.Date(2026, time.September, 2, 9, 0, 0, 0, time.UTC), *video.ScheduledStartAt)

	var short Short

	require.NoError(t, jsonv2.Unmarshal([]byte(wantShortJSON), &short))
	require.Equal(t, "short:shrt0000002", short.CanonicalPostID)

	var community Community

	require.NoError(t, jsonv2.Unmarshal([]byte(wantCommunityJSON), &community))
	require.Equal(t, "community:UgkxPOST1", community.CanonicalPostID)

	for _, tc := range []struct {
		value any
		want  string
	}{{video, wantVideoJSON}, {short, wantShortJSON}, {community, wantCommunityJSON}} {
		got, err := jsonv2.Marshal(tc.value)
		require.NoError(t, err)
		require.Equal(t, tc.want, string(got))
	}

	// 쇼츠 payload도 영상 renderer가 Video로 읽는다. canonical_post_id는 무시되고 영상 필드는 같다.
	var shortAsVideo Video

	require.NoError(t, jsonv2.Unmarshal([]byte(wantShortJSON), &shortAsVideo))
	require.Equal(t, short.VideoFields, shortAsVideo.VideoFields)
}
