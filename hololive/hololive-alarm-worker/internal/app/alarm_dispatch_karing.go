package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/iris-client-go/iris"
)

func buildAlarmDispatchKaringContentListRequest(group alarmDispatchGroup) (iris.KaringContentListRequest, error) {
	items, err := buildAlarmDispatchKaringContentItems(group)
	if err != nil {
		return iris.KaringContentListRequest{}, err
	}
	if len(items) == 0 {
		return iris.KaringContentListRequest{}, fmt.Errorf("build alarm dispatch karing content list request: items are empty")
	}
	return iris.KaringContentListRequest{
		ReceiverName: group.roomID,
		Items:        items,
		ExtraArgs:    buildAlarmDispatchKaringExtraArgs(group, len(items)),
	}, nil
}

func buildAlarmDispatchKaringContentItems(group alarmDispatchGroup) ([]iris.KaringContentItem, error) {
	if len(group.envelopes) > 0 && group.envelopes[0].SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox {
		return buildAlarmDispatchYouTubeOutboxKaringContentItems(group.envelopes[0])
	}
	items := make([]iris.KaringContentItem, 0, len(group.notifications))
	for _, notification := range group.notifications {
		items = append(items, buildAlarmDispatchNotificationKaringContentItem(notification))
	}
	return items, nil
}

func buildAlarmDispatchNotificationKaringContentItem(notification domain.AlarmNotification) iris.KaringContentItem {
	memberName := resolveAlarmDispatchMemberName(notification)
	return iris.KaringContentItem{
		Title:        resolveAlarmDispatchTitle(notification),
		URL:          resolveAlarmDispatchURL(notification),
		MemberName:   memberName,
		ChannelName:  resolveAlarmDispatchKaringChannelName(notification, memberName),
		Status:       resolveAlarmDispatchKaringStatus(notification),
		StartAt:      resolveAlarmDispatchKaringStartAt(notification.Stream),
		ThumbnailURL: resolveAlarmDispatchKaringThumbnailURL(notification),
		Platform:     resolveAlarmDispatchKaringPlatform(notification.Stream),
	}
}

func buildAlarmDispatchKaringExtraArgs(group alarmDispatchGroup, itemCount int) iris.KaringTemplateArgs {
	if len(group.envelopes) > 0 && group.envelopes[0].SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox {
		return buildAlarmDispatchOutboxKaringExtraArgs(group.envelopes[0], itemCount)
	}
	args := iris.KaringTemplateArgs{}
	if group.minutesUntil > 0 {
		args["alarm_title"] = fmt.Sprintf("방송 %d분 전 알림", group.minutesUntil)
		args["time_left"] = fmt.Sprintf("%d분 후 시작", group.minutesUntil)
		return args
	}
	args["alarm_title"] = "라이브 시작"
	args["time_left"] = "지금 시작"
	return args
}

func buildAlarmDispatchOutboxKaringExtraArgs(envelope domain.AlarmQueueEnvelope, itemCount int) iris.KaringTemplateArgs {
	if envelope.YouTubeOutbox == nil {
		return nil
	}
	baseTitle, timeLeft := alarmDispatchOutboxKaringLabels(envelope.YouTubeOutbox.Kind)
	return iris.KaringTemplateArgs{
		"alarm_title": alarmDispatchKaringTitleWithCount(baseTitle, itemCount),
		"time_left":   timeLeft,
	}
}

type alarmDispatchKaringLabel struct {
	alarmTitle string
	timeLeft   string
}

var alarmDispatchOutboxKaringLabelsByKind = map[domain.OutboxKind]alarmDispatchKaringLabel{
	domain.OutboxKindCommunityPost: {alarmTitle: "커뮤니티 알림", timeLeft: "새 커뮤니티"},
	domain.OutboxKindNewShort:      {alarmTitle: "쇼츠 알림", timeLeft: "새 쇼츠"},
	domain.OutboxKindNewVideo:      {alarmTitle: "새 영상", timeLeft: "새 영상"},
	domain.OutboxKindLiveStream:    {alarmTitle: "방송 알림", timeLeft: "방송 알림"},
}

func alarmDispatchOutboxKaringLabels(kind domain.OutboxKind) (string, string) {
	label, ok := alarmDispatchOutboxKaringLabelsByKind[kind]
	if !ok {
		label = alarmDispatchKaringLabel{alarmTitle: "알림", timeLeft: "새 알림"}
	}
	return label.alarmTitle, label.timeLeft
}

func alarmDispatchKaringTitleWithCount(title string, itemCount int) string {
	if itemCount <= 1 {
		return title
	}
	return fmt.Sprintf("%s · %d건", title, itemCount)
}

func resolveAlarmDispatchKaringChannelName(notification domain.AlarmNotification, fallback string) string {
	if notification.Stream != nil && strings.TrimSpace(notification.Stream.ChannelName) != "" {
		return strings.TrimSpace(notification.Stream.ChannelName)
	}
	return fallback
}

func resolveAlarmDispatchKaringStatus(notification domain.AlarmNotification) string {
	if notification.Stream != nil {
		if notification.Stream.Status == domain.StreamStatusLive || notification.Stream.StartActual != nil {
			return string(iris.KaringStreamStatusLive)
		}
	}
	if notification.MinutesUntil > 0 {
		return string(iris.KaringStreamStatusUpcoming)
	}
	return string(iris.KaringStreamStatusLive)
}

func resolveAlarmDispatchKaringStartAt(stream *domain.Stream) string {
	if stream == nil {
		return ""
	}
	if stream.StartActual != nil {
		return stream.StartActual.UTC().Format(time.RFC3339)
	}
	if stream.StartScheduled != nil {
		return stream.StartScheduled.UTC().Format(time.RFC3339)
	}
	return ""
}

func resolveAlarmDispatchKaringThumbnailURL(notification domain.AlarmNotification) string {
	if notification.Stream != nil && notification.Stream.Thumbnail != nil && strings.TrimSpace(*notification.Stream.Thumbnail) != "" {
		return strings.TrimSpace(*notification.Stream.Thumbnail)
	}
	if notification.Channel != nil {
		return strings.TrimSpace(notification.Channel.GetPhotoURL())
	}
	return ""
}

func resolveAlarmDispatchKaringPlatform(stream *domain.Stream) string {
	if stream == nil {
		return "youtube"
	}
	if stream.IsTwitchOnly {
		return "twitch"
	}
	if stream.IsChzzkOnly {
		return "chzzk"
	}
	return "youtube"
}

type alarmDispatchKaringVideoPayload struct {
	VideoID          string     `json:"video_id"`
	Title            string     `json:"title"`
	PublishedAt      *time.Time `json:"published_at,omitempty"`
	ScheduledStartAt *time.Time `json:"scheduled_start_at,omitempty"`
}

type alarmDispatchKaringCommunityPayload struct {
	PostID      string     `json:"post_id"`
	ContentText string     `json:"content_text"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

func buildAlarmDispatchYouTubeOutboxKaringContentItems(envelope domain.AlarmQueueEnvelope) ([]iris.KaringContentItem, error) {
	if envelope.YouTubeOutbox == nil {
		return nil, fmt.Errorf("build youtube outbox karing content list request: payload is nil")
	}
	if err := envelope.YouTubeOutbox.Validate(); err != nil {
		return nil, fmt.Errorf("build youtube outbox karing content list request: %w", err)
	}
	items := make([]iris.KaringContentItem, 0, len(envelope.YouTubeOutbox.Items))
	for _, item := range envelope.YouTubeOutbox.Items {
		contentItem, err := buildAlarmDispatchYouTubeOutboxKaringContentItem(*envelope.YouTubeOutbox, item)
		if err != nil {
			return nil, err
		}
		items = append(items, contentItem)
	}
	return items, nil
}

func buildAlarmDispatchYouTubeOutboxKaringContentItem(
	payload domain.YouTubeOutboxDispatchPayload,
	item domain.YouTubeOutboxItem,
) (iris.KaringContentItem, error) {
	switch payload.Kind {
	case domain.OutboxKindNewVideo, domain.OutboxKindNewShort, domain.OutboxKindLiveStream:
		return buildAlarmDispatchVideoOutboxKaringContentItem(payload, item)
	case domain.OutboxKindCommunityPost:
		return buildAlarmDispatchCommunityOutboxKaringContentItem(payload, item)
	default:
		return iris.KaringContentItem{}, fmt.Errorf("build youtube outbox karing content list request: unsupported kind %s", payload.Kind)
	}
}

func buildAlarmDispatchVideoOutboxKaringContentItem(
	payload domain.YouTubeOutboxDispatchPayload,
	item domain.YouTubeOutboxItem,
) (iris.KaringContentItem, error) {
	var data alarmDispatchKaringVideoPayload
	if err := json.Unmarshal([]byte(item.Payload), &data); err != nil {
		return iris.KaringContentItem{}, fmt.Errorf("build youtube video karing content list request: unmarshal payload: %w", err)
	}
	videoID := firstNonEmptyString(data.VideoID, item.ContentID)
	return iris.KaringContentItem{
		Title:       firstNonEmptyString(data.Title, "제목 없음"),
		URL:         alarmDispatchVideoOutboxURL(payload.Kind, videoID),
		MemberName:  resolveAlarmDispatchOutboxMemberName(payload),
		ChannelName: resolveAlarmDispatchOutboxMemberName(payload),
		Status:      alarmDispatchVideoOutboxStatus(payload.Kind, data),
		StartAt:     alarmDispatchKaringTimeString(firstNonNilTime(data.ScheduledStartAt, data.PublishedAt)),
		Platform:    "youtube",
	}, nil
}

func buildAlarmDispatchCommunityOutboxKaringContentItem(
	payload domain.YouTubeOutboxDispatchPayload,
	item domain.YouTubeOutboxItem,
) (iris.KaringContentItem, error) {
	var data alarmDispatchKaringCommunityPayload
	if err := json.Unmarshal([]byte(item.Payload), &data); err != nil {
		return iris.KaringContentItem{}, fmt.Errorf("build youtube community karing content list request: unmarshal payload: %w", err)
	}
	memberName := resolveAlarmDispatchOutboxMemberName(payload)
	postID := firstNonEmptyString(data.PostID, item.ContentID)
	return iris.KaringContentItem{
		Title:       firstNonEmptyString(firstNonEmptyLine(data.ContentText), "커뮤니티 알림"),
		URL:         fmt.Sprintf("https://www.youtube.com/post/%s", postID),
		MemberName:  memberName,
		ChannelName: memberName,
		Status:      "커뮤니티",
		StartAt:     alarmDispatchKaringTimeString(data.PublishedAt),
		Platform:    "youtube",
	}, nil
}

func resolveAlarmDispatchOutboxMemberName(payload domain.YouTubeOutboxDispatchPayload) string {
	if memberName := strings.TrimSpace(payload.MemberName); memberName != "" {
		return memberName
	}
	return "VTuber"
}

func alarmDispatchVideoOutboxURL(kind domain.OutboxKind, videoID string) string {
	if kind == domain.OutboxKindNewShort {
		return fmt.Sprintf("https://www.youtube.com/shorts/%s", videoID)
	}
	return fmt.Sprintf("https://youtu.be/%s", videoID)
}

var alarmDispatchVideoOutboxStatusByKind = map[domain.OutboxKind]string{
	domain.OutboxKindNewShort: "쇼츠",
	domain.OutboxKindNewVideo: "새 영상",
}

func alarmDispatchVideoOutboxStatus(kind domain.OutboxKind, data alarmDispatchKaringVideoPayload) string {
	if status, ok := alarmDispatchVideoOutboxStatusByKind[kind]; ok {
		return status
	}
	if kind != domain.OutboxKindLiveStream {
		return "알림"
	}
	return alarmDispatchLiveOutboxStatus(data.PublishedAt)
}

func alarmDispatchLiveOutboxStatus(publishedAt *time.Time) string {
	if publishedAt == nil {
		return string(iris.KaringStreamStatusUpcoming)
	}
	return string(iris.KaringStreamStatusLive)
}

func alarmDispatchKaringTimeString(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func firstNonNilTime(values ...*time.Time) *time.Time {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstNonEmptyLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
