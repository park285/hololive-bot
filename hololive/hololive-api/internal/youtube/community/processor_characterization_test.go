package community

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"reflect"
	"strings"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// 기대값은 parser DTO 왕복 경로(삭제 전)에서 그대로 통과한 값이다. 바꾸면 저장 행·outbox payload 계약이 바뀐다.
const (
	// 접두사·raw ID·URL 표현이 모두 합쳐지는 canonical ID다.
	aliasCanonicalID = "community:abc"
	defCanonicalID   = "community:def"
	cccCanonicalID   = "community:ccc"

	abcPayloadJSON = `{"post_id":"abc","channel_id":"UC_TEST","author_name":"Author B",` +
		`"author_photo":[{"url":"https://img.test/a.jpg","width":88,"height":88}],` +
		`"content_text":"first text","published_text":"1 day ago","published_at":"2026-04-10T01:11:12Z",` +
		`"like_count":7,"comment_count":5,"images":[{"url":"https://img.test/i.jpg","width":640,"height":480}],` +
		`"attached_video":"vid-1","first_seen_at":"0001-01-01T00:00:00Z","last_seen_at":"0001-01-01T00:00:00Z",` +
		`"canonical_post_id":"community:abc"}`
	defPayloadJSON = `{"post_id":"def","channel_id":"UC_TEST","content_text":"def text","like_count":0,"comment_count":0,` +
		`"first_seen_at":"0001-01-01T00:00:00Z","last_seen_at":"0001-01-01T00:00:00Z","canonical_post_id":"community:def"}`
	cccPayloadJSON = `{"post_id":"ccc","channel_id":"UC_TEST","author_name":"Known","like_count":1,"comment_count":2,` +
		`"first_seen_at":"0001-01-01T00:00:00Z","last_seen_at":"0001-01-01T00:00:00Z","canonical_post_id":"community:ccc"}`
)

var (
	characterizationDetectedAt = time.Date(2026, time.August, 13, 8, 0, 0, 0, time.UTC)
	aliasPublishedAt           = time.Date(2026, time.April, 10, 10, 11, 12, 0, time.FixedZone("KST", 9*60*60))
	conflictingPublishedAt     = time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
)

type characterizationCase struct {
	name              string
	payload           contract.CommunityPayloadV1
	notifyUnseen      bool
	known             map[string]struct{}
	wantIDs           []string
	wantPosts         []*domain.YouTubeCommunityPost
	wantNotifications []*domain.YouTubeNotificationOutbox
	wantTracking      []*domain.YouTubeContentAlarmTracking
	wantWatermark     *domain.YouTubeContentWatermark
}

func TestArtifactsFromPayloadCharacterizesCanonicalNormalization(t *testing.T) {
	for _, tc := range characterizationCases() {
		t.Run(tc.name, func(t *testing.T) {
			before := marshalPayload(t, tc.payload)

			window := NormalizeWindow(&tc.payload)
			ids := window.CanonicalPostIDs()
			batch := window.Artifacts(tc.notifyUnseen, tc.known, characterizationDetectedAt)

			requireSameElements(t, "canonical ids", ids, tc.wantIDs)
			requireSameElements(t, "posts", batch.Posts, tc.wantPosts)
			requireSameElements(t, "notifications", batch.Notifications, tc.wantNotifications)
			requireSameElements(t, "tracking", batch.Tracking, tc.wantTracking)

			if !reflect.DeepEqual(batch.Watermark, tc.wantWatermark) {
				t.Fatalf("watermark = %#v, want %#v", batch.Watermark, tc.wantWatermark)
			}

			requireNilEmptyThumbnails(t, batch.Posts)
			requirePublishedAtNotAliased(t, batch.Posts, tc.payload.Posts)

			if after := marshalPayload(t, tc.payload); !bytes.Equal(after, before) {
				t.Fatalf("payload mutated:\nbefore %s\nafter  %s", before, after)
			}
		})
	}
}

func characterizationCases() []characterizationCase {
	aliasIDs := []string{aliasCanonicalID, defCanonicalID, cccCanonicalID}
	wantPublishedAt := aliasPublishedAt.UTC()

	return []characterizationCase{
		{
			name:         "alias merge with known and unknown split",
			payload:      aliasWindowPayload(),
			notifyUnseen: true,
			known:        map[string]struct{}{cccCanonicalID: {}},
			wantIDs:      aliasIDs,
			wantPosts:    aliasWindowPosts(),
			wantNotifications: []*domain.YouTubeNotificationOutbox{
				pendingNotification(aliasCanonicalID, abcPayloadJSON),
				pendingNotification(defCanonicalID, defPayloadJSON),
			},
			wantTracking:  []*domain.YouTubeContentAlarmTracking{detectedTracking(aliasCanonicalID, &wantPublishedAt), detectedTracking(defCanonicalID, nil)},
			wantWatermark: aliasWindowWatermark(),
		},
		{
			name:         "nil known set notifies every canonical post",
			payload:      aliasWindowPayload(),
			notifyUnseen: true,
			wantIDs:      aliasIDs,
			wantPosts:    aliasWindowPosts(),
			wantNotifications: []*domain.YouTubeNotificationOutbox{
				pendingNotification(aliasCanonicalID, abcPayloadJSON),
				pendingNotification(defCanonicalID, defPayloadJSON),
				pendingNotification(cccCanonicalID, cccPayloadJSON),
			},
			wantTracking: []*domain.YouTubeContentAlarmTracking{
				detectedTracking(aliasCanonicalID, &wantPublishedAt),
				detectedTracking(defCanonicalID, nil),
				detectedTracking(cccCanonicalID, nil),
			},
			wantWatermark: aliasWindowWatermark(),
		},
		{
			name:          "all known posts emit no notification",
			payload:       aliasWindowPayload(),
			notifyUnseen:  true,
			known:         map[string]struct{}{aliasCanonicalID: {}, defCanonicalID: {}, cccCanonicalID: {}},
			wantIDs:       aliasIDs,
			wantPosts:     aliasWindowPosts(),
			wantWatermark: aliasWindowWatermark(),
		},
		{
			name:          "notifications disabled persists window only",
			payload:       aliasWindowPayload(),
			wantIDs:       aliasIDs,
			wantPosts:     aliasWindowPosts(),
			wantWatermark: aliasWindowWatermark(),
		},
		{
			name: "only invalid canonical ids",
			payload: contract.CommunityPayloadV1{
				ChannelID: testChannelID,
				Posts: []contract.CommunityPostV1{
					{PostID: "short:zzz", ChannelID: testChannelID},
					{PostID: "", ChannelID: testChannelID},
				},
			},
			notifyUnseen: true,
		},
		{
			name:         "empty payload",
			payload:      contract.CommunityPayloadV1{ChannelID: testChannelID},
			notifyUnseen: true,
		},
	}
}

// aliasWindowPayload는 검증 전 원본 순서의 창이다. 정렬하지 않고 첫 등장 순서를 유지해야 한다.
func aliasWindowPayload() contract.CommunityPayloadV1 {
	return contract.CommunityPayloadV1{
		ChannelID: testChannelID,
		Posts: []contract.CommunityPostV1{
			{
				PostID:       aliasCanonicalID,
				ChannelID:    testChannelID,
				AuthorPhoto:  []contract.Thumbnail{},
				ContentText:  "first text",
				CommentCount: 5,
			},
			{PostID: "short:zzz", ChannelID: testChannelID, ContentText: "prefix mismatch"},
			{
				PostID:      "def",
				ChannelID:   testChannelID,
				AuthorPhoto: []contract.Thumbnail{},
				ContentText: "def text",
			},
			{
				PostID:         "abc",
				UpstreamPostID: "up-abc",
				ChannelID:      testChannelID,
				AuthorID:       "UC_AUTHOR",
				AuthorName:     "Author B",
				AuthorPhoto:    []contract.Thumbnail{{URL: "https://img.test/a.jpg", Width: 88, Height: 88}},
				ContentText:    "second text",
				PublishedText:  "1 day ago",
				PublishedAt:    &aliasPublishedAt,
				LikeCount:      7,
				CommentCount:   9,
				Images:         []contract.Thumbnail{{URL: "https://img.test/i.jpg", Width: 640, Height: 480}},
				VideoID:        "vid-1",
			},
			{PostID: "   ", ChannelID: testChannelID, ContentText: "empty id"},
			{
				PostID:       "ccc",
				ChannelID:    "UC_OTHER",
				AuthorName:   "Known",
				LikeCount:    1,
				CommentCount: 2,
				Images:       []contract.Thumbnail{},
			},
			{
				PostID:        "https://www.youtube.com/post/abc?lc=1",
				ChannelID:     "UC_OTHER",
				AuthorName:    "Author C",
				AuthorPhoto:   []contract.Thumbnail{{URL: "https://img.test/c.jpg", Width: 48, Height: 48}},
				PublishedText: "2 days ago",
				PublishedAt:   &conflictingPublishedAt,
				LikeCount:     100,
				CommentCount:  200,
				Images:        []contract.Thumbnail{{URL: "https://img.test/c-image.jpg", Width: 1, Height: 1}},
				VideoID:       "vid-2",
			},
			{PostID: strings.Repeat("x", 41), ChannelID: testChannelID, ContentText: "too long"},
		},
	}
}

func aliasWindowPosts() []*domain.YouTubeCommunityPost {
	wantPublishedAt := aliasPublishedAt.UTC()

	return []*domain.YouTubeCommunityPost{
		{
			PostID:        aliasCanonicalID,
			ChannelID:     testChannelID,
			AuthorName:    "Author B",
			AuthorPhoto:   domain.ThumbnailsJSON{{URL: "https://img.test/a.jpg", Width: 88, Height: 88}},
			ContentText:   "first text",
			PublishedText: "1 day ago",
			PublishedAt:   &wantPublishedAt,
			LikeCount:     7,
			CommentCount:  5,
			Images:        domain.ThumbnailsJSON{{URL: "https://img.test/i.jpg", Width: 640, Height: 480}},
			AttachedVideo: "vid-1",
		},
		{
			PostID:      defCanonicalID,
			ChannelID:   testChannelID,
			ContentText: "def text",
		},
		{
			PostID:       cccCanonicalID,
			ChannelID:    testChannelID,
			AuthorName:   "Known",
			LikeCount:    1,
			CommentCount: 2,
		},
	}
}

func aliasWindowWatermark() *domain.YouTubeContentWatermark {
	return &domain.YouTubeContentWatermark{
		ChannelID:     testChannelID,
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: aliasCanonicalID,
	}
}

func pendingNotification(contentID, payload string) *domain.YouTubeNotificationOutbox {
	return &domain.YouTubeNotificationOutbox{
		Kind:      domain.OutboxKindCommunityPost,
		ChannelID: testChannelID,
		ContentID: contentID,
		Payload:   payload,
		Status:    domain.OutboxStatusPending,
	}
}

func detectedTracking(contentID string, publishedAt *time.Time) *domain.YouTubeContentAlarmTracking {
	return &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         contentID,
		ChannelID:         testChannelID,
		ActualPublishedAt: publishedAt,
		DetectedAt:        characterizationDetectedAt,
	}
}

func marshalPayload(t *testing.T, payload contract.CommunityPayloadV1) []byte {
	t.Helper()

	data, err := jsonv2.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	return data
}

// requireNilEmptyThumbnails는 빈 썸네일 목록이 빈 배열이 아니라 nil(NULL·생략)로 남는지 확인한다.
func requireNilEmptyThumbnails(t *testing.T, posts []*domain.YouTubeCommunityPost) {
	t.Helper()

	for i, post := range posts {
		if post.AuthorPhoto != nil && len(post.AuthorPhoto) == 0 {
			t.Fatalf("posts[%d].AuthorPhoto is empty non-nil; want nil for empty thumbnails", i)
		}

		if post.Images != nil && len(post.Images) == 0 {
			t.Fatalf("posts[%d].Images is empty non-nil; want nil for empty thumbnails", i)
		}
	}
}

// requirePublishedAtNotAliased는 저장 행의 published_at이 payload 포인터를 공유하지 않는지 확인한다.
func requirePublishedAtNotAliased(t *testing.T, posts []*domain.YouTubeCommunityPost, sources []contract.CommunityPostV1) {
	t.Helper()

	for i, post := range posts {
		if post.PublishedAt == nil {
			continue
		}

		for j := range sources {
			if post.PublishedAt == sources[j].PublishedAt {
				t.Fatalf("posts[%d].PublishedAt aliases payload post %d", i, j)
			}
		}
	}
}

func requireSameElements[T any](t *testing.T, label string, got, want []T) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("%s len = %d, want %d: got %#v", label, len(got), len(want), got)
	}

	for i := range want {
		if !reflect.DeepEqual(got[i], want[i]) {
			t.Fatalf("%s[%d] = %#v, want %#v", label, i, got[i], want[i])
		}
	}
}
