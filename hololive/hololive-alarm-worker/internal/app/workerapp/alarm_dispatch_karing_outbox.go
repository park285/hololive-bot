package workerapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/iris-client-go/iris"
	json "github.com/park285/shared-go/pkg/json"
)

type alarmDispatchKaringVideoPayload struct {
	VideoID          string                `json:"video_id"`
	Title            string                `json:"title"`
	Thumbnail        domain.ThumbnailsJSON `json:"thumbnail,omitempty"`
	PublishedAt      *time.Time            `json:"published_at,omitempty"`
	ScheduledStartAt *time.Time            `json:"scheduled_start_at,omitempty"`
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

func buildAlarmDispatchYouTubeOutboxKaringContentItems(envelope *domain.AlarmQueueEnvelope) ([]iris.KaringContentItem, error) {
	if envelope == nil || envelope.YouTubeOutbox == nil {
		return nil, fmt.Errorf("build youtube outbox karing content list request: payload is nil")
	}
	if err := envelope.YouTubeOutbox.Validate(); err != nil {
		return nil, fmt.Errorf("build youtube outbox karing content list request: %w", err)
	}
	items := make([]iris.KaringContentItem, 0, len(envelope.YouTubeOutbox.Items))
	for _, item := range envelope.YouTubeOutbox.Items {
		contentItem, err := buildAlarmDispatchYouTubeOutboxKaringContentItem(envelope.YouTubeOutbox, item)
		if err != nil {
			return nil, err
		}
		items = append(items, contentItem)
	}
	return items, nil
}

func buildAlarmDispatchYouTubeOutboxKaringContentItem(
	payload *domain.YouTubeOutboxDispatchPayload,
	item domain.YouTubeOutboxItem,
) (iris.KaringContentItem, error) {
	if payload == nil {
		return iris.KaringContentItem{}, fmt.Errorf("build youtube outbox karing content list request: payload is nil")
	}
	switch payload.Kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort, domain.OutboxKindLiveStream:
		return buildAlarmDispatchVideoOutboxKaringContentItem(payload, item)
	case domain.OutboxKindCommunityPost:
		return buildAlarmDispatchCommunityOutboxKaringContentItem(payload, item)
	case domain.OutboxKindMilestone:
		return iris.KaringContentItem{}, fmt.Errorf("build youtube outbox karing content list request: milestone outbox is not supported")
	default:
		return iris.KaringContentItem{}, fmt.Errorf("build youtube outbox karing content list request: unsupported kind %s", payload.Kind)
	}
}

func buildAlarmDispatchVideoOutboxKaringContentItem(
	payload *domain.YouTubeOutboxDispatchPayload,
	item domain.YouTubeOutboxItem,
) (iris.KaringContentItem, error) {
	var data alarmDispatchKaringVideoPayload
	if err := json.Unmarshal([]byte(item.Payload), &data); err != nil {
		return iris.KaringContentItem{}, fmt.Errorf("build youtube video karing content list request: unmarshal payload: %w", err)
	}
	videoID := firstNonEmptyString(data.VideoID, item.ContentID)
	return iris.KaringContentItem{
		Title:        firstNonEmptyString(data.Title, "제목 없음"),
		URL:          alarmDispatchVideoOutboxURL(payload.Kind, videoID),
		MemberName:   resolveAlarmDispatchOutboxMemberName(payload),
		ChannelName:  resolveAlarmDispatchOutboxMemberName(payload),
		Status:       alarmDispatchVideoOutboxStatus(payload.Kind, data),
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
	payload *domain.YouTubeOutboxDispatchPayload,
	item domain.YouTubeOutboxItem,
) (iris.KaringContentItem, error) {
	var data alarmDispatchKaringCommunityPayload
	if err := json.Unmarshal([]byte(item.Payload), &data); err != nil {
		return iris.KaringContentItem{}, fmt.Errorf("build youtube community karing content list request: unmarshal payload: %w", err)
	}
	memberName := resolveAlarmDispatchOutboxMemberName(payload)
	postID := firstNonEmptyString(data.PostID, item.ContentID)
	return iris.KaringContentItem{
		Title:        firstNonEmptyString(cleanCommunityOutboxTitle(data.ContentText), "커뮤니티 알림"),
		URL:          fmt.Sprintf("https://www.youtube.com/post/%s", postID),
		MemberName:   memberName,
		ChannelName:  memberName,
		Status:       iris.KaringStreamStatus("커뮤니티"),
		StartAt:      alarmDispatchKaringTimeString(data.PublishedAt),
		ThumbnailURL: communityOutboxThumbnailURL(&data),
		Platform:     "youtube",
	}, nil
}

func resolveAlarmDispatchOutboxMemberName(payload *domain.YouTubeOutboxDispatchPayload) string {
	if payload == nil {
		return "VTuber"
	}
	if memberName := strings.TrimSpace(payload.MemberName); memberName != "" {
		return memberName
	}
	return "VTuber"
}

func alarmDispatchVideoOutboxURL(kind domain.OutboxKind, videoID string) string {
	if kind == domain.OutboxKindNewShort {
		return fmt.Sprintf("https://www.youtube.com/shorts/%s", videoID)
	}
	return domain.YouTubeWatchURL(videoID)
}

var alarmDispatchVideoOutboxStatusByKind = map[domain.OutboxKind]iris.KaringStreamStatus{
	domain.OutboxKindNewShort: iris.KaringStreamStatus("쇼츠"),
	domain.OutboxKindNewVideo: iris.KaringStreamStatus("새 영상"),
}

func alarmDispatchVideoOutboxStatus(kind domain.OutboxKind, data alarmDispatchKaringVideoPayload) iris.KaringStreamStatus {
	if status, ok := alarmDispatchVideoOutboxStatusByKind[kind]; ok {
		return status
	}
	if kind != domain.OutboxKindLiveStream {
		return iris.KaringStreamStatus("알림")
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
