package workerapp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	json "github.com/park285/hololive-bot/shared-go/pkg/json"
	"github.com/park285/iris-client-go/iris"
)

const alarmDispatchKaringMaxItemsPerRequest = 4

var alarmDispatchKaringDisplayLocation = time.FixedZone("KST", 9*60*60)

var alarmDispatchKaringTemplateIDByItemCount = map[int]int64{
	1: 133266,
	2: 133223,
	3: 133222,
	4: 133267,
}

func buildAlarmDispatchKaringContentListRequests(group alarmDispatchGroup) ([]iris.KaringContentListRequest, error) {
	items, err := buildAlarmDispatchKaringContentItems(group)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("build alarm dispatch karing content list request: items are empty")
	}
	requests := make([]iris.KaringContentListRequest, 0, (len(items)+alarmDispatchKaringMaxItemsPerRequest-1)/alarmDispatchKaringMaxItemsPerRequest)
	for start := 0; start < len(items); start += alarmDispatchKaringMaxItemsPerRequest {
		end := min(start+alarmDispatchKaringMaxItemsPerRequest, len(items))
		chunk := items[start:end]
		req := iris.KaringContentListRequest{
			ClientRequestID: new(alarmDispatchClientRequestID(group, start, end)),
			Items:           chunk,
			ExtraArgs:       buildAlarmDispatchKaringExtraArgs(group, len(chunk)),
			TemplateID:      alarmDispatchKaringTemplateID(len(chunk)),
		}
		applyAlarmDispatchKaringReceiver(&req, group.roomID)
		requests = append(requests, req)
	}
	return requests, nil
}

func alarmDispatchClientRequestID(group alarmDispatchGroup, start, end int) string {
	parts := []string{
		"alarm-dispatch-v1",
		strings.TrimSpace(group.roomID),
		strconv.Itoa(start),
		strconv.Itoa(end),
	}
	for _, envelope := range group.envelopes {
		parts = append(parts, alarmDispatchEnvelopeClientRequestIDParts(envelope)...)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "hololive-alarm:" + hex.EncodeToString(sum[:16])
}

func alarmDispatchEnvelopeClientRequestIDParts(envelope domain.AlarmQueueEnvelope) []string {
	parts := make([]string, 0, 6+len(envelope.ClaimKeys))
	parts = append(parts,
		strconv.FormatInt(envelope.DispatchOutboxID, 10),
		string(envelope.SourceKind),
		string(envelope.Notification.AlarmType),
		strconv.Itoa(envelope.Notification.MinutesUntil),
	)
	parts = append(parts, envelope.ClaimKeys...)
	if envelope.YouTubeOutbox != nil {
		parts = append(parts, envelope.YouTubeOutbox.Identity())
		for _, item := range envelope.YouTubeOutbox.Items {
			parts = append(parts, strconv.FormatInt(item.OutboxID, 10), item.ContentID)
		}
	}
	return parts
}

func applyAlarmDispatchKaringReceiver(req *iris.KaringContentListRequest, roomID string) {
	if req == nil {
		return
	}
	trimmed := strings.TrimSpace(roomID)
	if receiverRoomID, err := strconv.ParseInt(trimmed, 10, 64); err == nil && receiverRoomID > 0 {
		req.ReceiverRoomID = receiverRoomID
		return
	}
	req.ReceiverName = trimmed
}

func alarmDispatchKaringTemplateID(itemCount int) int64 {
	return alarmDispatchKaringTemplateIDByItemCount[itemCount]
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
		Status:       "",
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

func resolveAlarmDispatchKaringStartAt(stream *domain.Stream) string {
	if stream == nil {
		return ""
	}
	if stream.StartActual != nil {
		return alarmDispatchKaringTimeString(stream.StartActual)
	}
	if stream.StartScheduled != nil {
		return alarmDispatchKaringTimeString(stream.StartScheduled)
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
		Title:        firstNonEmptyString(data.Title, "제목 없음"),
		URL:          alarmDispatchVideoOutboxURL(payload.Kind, videoID),
		MemberName:   resolveAlarmDispatchOutboxMemberName(payload),
		ChannelName:  resolveAlarmDispatchOutboxMemberName(payload),
		Status:       alarmDispatchVideoOutboxStatus(payload.Kind, data),
		StartAt:      alarmDispatchKaringTimeString(firstNonNilTime(data.ScheduledStartAt, data.PublishedAt)),
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
		Title:        firstNonEmptyString(cleanCommunityOutboxTitle(data.ContentText), "커뮤니티 알림"),
		URL:          fmt.Sprintf("https://www.youtube.com/post/%s", postID),
		MemberName:   memberName,
		ChannelName:  memberName,
		Status:       iris.KaringStreamStatus("커뮤니티"),
		StartAt:      alarmDispatchKaringTimeString(data.PublishedAt),
		ThumbnailURL: communityOutboxThumbnailURL(data),
		Platform:     "youtube",
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
	return fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)
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
