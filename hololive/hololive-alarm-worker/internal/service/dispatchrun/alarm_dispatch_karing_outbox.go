package dispatchrun

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/park285/iris-client-go/v2/iris"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/util"
)

type alarmDispatchKaringVideoPayload struct {
	VideoID          string                `json:"video_id"`
	Title            string                `json:"title"`
	Thumbnail        domain.ThumbnailsJSON `json:"thumbnail,omitempty"`
	PublishedAt      *time.Time            `json:"published_at,omitempty"`
	ScheduledStartAt *time.Time            `json:"scheduled_start_at,omitempty"`
	IsPremiere       *bool                 `json:"is_premiere,omitempty"`
}

type alarmDispatchKaringCommunityPayload struct {
	PostID      string                            `json:"post_id"`
	ContentText string                            `json:"content_text"`
	Images      []alarmDispatchKaringImagePayload `json:"images,omitempty"`
	AuthorPhoto []alarmDispatchKaringImagePayload `json:"author_photo,omitempty"`
	PublishedAt *time.Time                        `json:"published_at,omitempty"`
}

type alarmDispatchKaringImagePayload struct {
	URL    string `json:"url"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
}

func buildAlarmDispatchYouTubeOutboxKaringItems(ctx context.Context, messageStrings *messagestrings.Store, envelope *domain.AlarmQueueEnvelope) ([]alarmDispatchKaringItem, error) {
	if envelope == nil || envelope.YouTubeOutbox == nil {
		return nil, errors.New("build youtube outbox karing content list request: payload is nil")
	}

	if err := envelope.YouTubeOutbox.Validate(); err != nil {
		return nil, fmt.Errorf("build youtube outbox karing content list request: %w", err)
	}

	entries := make([]alarmDispatchKaringItem, 0, len(envelope.YouTubeOutbox.Items))
	for i := range envelope.YouTubeOutbox.Items {
		item := envelope.YouTubeOutbox.Items[i]

		contentItem, err := buildAlarmDispatchYouTubeOutboxKaringContentItem(ctx, messageStrings, envelope.YouTubeOutbox, item)
		if err != nil {
			return nil, fmt.Errorf("build alarm dispatch youtube outbox karing content item: %w", err)
		}

		entries = append(entries, alarmDispatchKaringItem{
			identity: alarmDispatchOutboxKaringItemIdentity(&envelope.YouTubeOutbox.Items[i]),
			item:     contentItem,
		})
	}

	return entries, nil
}

func buildAlarmDispatchYouTubeOutboxKaringContentItem(
	ctx context.Context,
	messageStrings *messagestrings.Store,
	payload *domain.YouTubeOutboxDispatchPayload,
	item domain.YouTubeOutboxItem,
) (iris.KaringContentItem, error) {
	if payload == nil {
		return iris.KaringContentItem{}, errors.New("build youtube outbox karing content list request: payload is nil")
	}

	if payload.Kind == domain.OutboxKindMilestone {
		return iris.KaringContentItem{}, errors.New("build youtube outbox karing content list request: milestone outbox is not supported")
	}

	type contentItemBuilder struct {
		action string
		build  func(context.Context, *messagestrings.Store, *domain.YouTubeOutboxDispatchPayload, domain.YouTubeOutboxItem) (iris.KaringContentItem, error)
	}

	builders := map[domain.OutboxKind]contentItemBuilder{
		domain.OutboxKindNewVideo:      {action: "video", build: buildAlarmDispatchVideoOutboxKaringContentItem},
		domain.OutboxKindNewShort:      {action: "video", build: buildAlarmDispatchVideoOutboxKaringContentItem},
		domain.OutboxKindLiveStream:    {action: "video", build: buildAlarmDispatchVideoOutboxKaringContentItem},
		domain.OutboxKindCommunityPost: {action: "community", build: buildAlarmDispatchCommunityOutboxKaringContentItem},
	}
	selected, ok := builders[payload.Kind]

	if !ok {
		return iris.KaringContentItem{}, fmt.Errorf("build youtube outbox karing content list request: unsupported kind %s", payload.Kind)
	}

	out, err := selected.build(ctx, messageStrings, payload, item)
	if err != nil {
		return out, fmt.Errorf("build alarm dispatch %s outbox karing content item: %w", selected.action, err)
	}

	return out, nil
}

func buildAlarmDispatchVideoOutboxKaringContentItem(
	ctx context.Context,
	messageStrings *messagestrings.Store,
	payload *domain.YouTubeOutboxDispatchPayload,
	item domain.YouTubeOutboxItem,
) (iris.KaringContentItem, error) {
	var data alarmDispatchKaringVideoPayload

	if err := jsonv2.Unmarshal([]byte(item.Payload), &data); err != nil {
		return iris.KaringContentItem{}, fmt.Errorf("build youtube video karing content list request: unmarshal payload: %w", err)
	}

	videoID := firstNonEmptyString(data.VideoID, item.ContentID)
	memberName := resolveAlarmDispatchOutboxMemberName(ctx, messageStrings, payload)

	return iris.KaringContentItem{
		Title:        firstNonEmptyString(data.Title, alarmDispatchMessageString(ctx, messageStrings, "alarm_no_title", "제목 없음")),
		URL:          alarmDispatchVideoOutboxURL(payload.Kind, videoID),
		MemberName:   memberName,
		ChannelName:  memberName,
		Status:       alarmDispatchVideoOutboxStatus(ctx, messageStrings, payload.Kind, data),
		StartAt:      alarmDispatchKaringTimeString(util.FirstNonNilTime(data.ScheduledStartAt, data.PublishedAt)),
		ThumbnailURL: bestKaringThumbnailURL(data.Thumbnail),
		Platform:     "youtube",
	}, nil
}

func bestKaringThumbnailURL(thumbnails domain.ThumbnailsJSON) string {
	bestURL := ""
	bestArea := -1

	for _, thumbnail := range thumbnails {
		url := normalizeKaringImageURL(thumbnail.URL)
		if url == "" {
			continue
		}

		area := thumbnail.Width * thumbnail.Height
		if area > bestArea {
			bestURL = url
			bestArea = area
		}
	}

	return bestURL
}

func buildAlarmDispatchCommunityOutboxKaringContentItem(
	ctx context.Context,
	messageStrings *messagestrings.Store,
	payload *domain.YouTubeOutboxDispatchPayload,
	item domain.YouTubeOutboxItem,
) (iris.KaringContentItem, error) {
	var data alarmDispatchKaringCommunityPayload

	if err := jsonv2.Unmarshal([]byte(item.Payload), &data); err != nil {
		return iris.KaringContentItem{}, fmt.Errorf("build youtube community karing content list request: unmarshal payload: %w", err)
	}

	memberName := resolveAlarmDispatchOutboxMemberName(ctx, messageStrings, payload)
	postID := firstNonEmptyString(data.PostID, item.ContentID)

	return iris.KaringContentItem{
		Title:        firstNonEmptyString(cleanCommunityOutboxTitle(data.ContentText), messageStrings.GetOrContext(ctx, messagestrings.NamespaceKaring, "item_title_community_fallback", "커뮤니티 알림")),
		URL:          fmt.Sprintf("https://www.youtube.com/post/%s", postID),
		MemberName:   memberName,
		ChannelName:  memberName,
		Status:       iris.KaringStreamStatus(messageStrings.GetOrContext(ctx, messagestrings.NamespaceKaring, "status_community", "커뮤니티")),
		StartAt:      alarmDispatchKaringTimeString(data.PublishedAt),
		ThumbnailURL: communityOutboxThumbnailURL(&data),
		Platform:     "youtube",
	}, nil
}

func resolveAlarmDispatchOutboxMemberName(ctx context.Context, messageStrings *messagestrings.Store, payload *domain.YouTubeOutboxDispatchPayload) string {
	if payload == nil {
		return messageStrings.VTuberFallbackContext(ctx)
	}

	if memberName := strings.TrimSpace(payload.MemberName); memberName != "" {
		return memberName
	}

	return messageStrings.VTuberFallbackContext(ctx)
}

func alarmDispatchVideoOutboxURL(kind domain.OutboxKind, videoID string) string {
	if kind == domain.OutboxKindNewShort {
		return fmt.Sprintf("https://www.youtube.com/shorts/%s", videoID)
	}

	return domain.YouTubeWatchURL(videoID)
}

type alarmDispatchVideoOutboxStatusLabel struct {
	key      string
	fallback string
}

var alarmDispatchVideoOutboxStatusByKind = map[domain.OutboxKind]alarmDispatchVideoOutboxStatusLabel{
	domain.OutboxKindNewShort: {key: "status_shorts", fallback: "쇼츠"},
	domain.OutboxKindNewVideo: {key: "status_video", fallback: "새 영상"},
}

func alarmDispatchVideoOutboxStatus(ctx context.Context, messageStrings *messagestrings.Store, kind domain.OutboxKind, data alarmDispatchKaringVideoPayload) iris.KaringStreamStatus {
	if kind == domain.OutboxKindNewVideo && data.IsPremiere != nil && *data.IsPremiere {
		return iris.KaringStreamStatus(messageStrings.GetOrContext(ctx, messagestrings.NamespaceKaring, "status_video_premiere", "최초공개"))
	}

	if label, ok := alarmDispatchVideoOutboxStatusByKind[kind]; ok {
		return iris.KaringStreamStatus(messageStrings.GetOrContext(ctx, messagestrings.NamespaceKaring, label.key, label.fallback))
	}

	if kind != domain.OutboxKindLiveStream {
		return iris.KaringStreamStatus(messageStrings.GetOrContext(ctx, messagestrings.NamespaceKaring, "status_fallback", "알림"))
	}

	return alarmDispatchLiveOutboxStatus(data.PublishedAt)
}

func alarmDispatchLiveOutboxStatus(publishedAt *time.Time) iris.KaringStreamStatus {
	if publishedAt == nil {
		return iris.KaringStreamStatusUpcoming
	}

	return iris.KaringStreamStatusLive
}

func alarmDispatchKaringTimeString(value *time.Time) string {
	if value == nil {
		return ""
	}

	return value.In(alarmDispatchKaringDisplayLocation).Format("01/02 15:04")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}

	return ""
}
