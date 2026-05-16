package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
	"github.com/park285/iris-client-go/iris"
)

type alarmDispatchConsumer interface {
	DrainBatch(ctx context.Context, maxItems int) ([]domain.AlarmQueueEnvelope, error)
	MarkSending(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	MarkDispatched(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	ReleaseClaimKeys(ctx context.Context, claimKeys []string) error
	ScheduleRetry(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	MoveToDLQ(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
	Requeue(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) error
}

type alarmDispatchQuarantineConsumer interface {
	Quarantine(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, reason string) error
}

type alarmDispatchIdleWaiter interface {
	Wait(ctx context.Context) bool
	Reset()
}

type alarmDispatchSender interface {
	SendMessage(ctx context.Context, roomID, message string) error
	SendKaringContentList(ctx context.Context, roomID string, req iris.KaringContentListRequest) error
}

type alarmDispatchRunner struct {
	consumer           alarmDispatchConsumer
	sender             alarmDispatchSender
	idleWaiter         alarmDispatchIdleWaiter
	karingEnabled      bool
	consumerMode       string
	postSendQuarantine bool
	maxBatch           int
	maxBatchesPerWake  int
	batchesSinceWake   int
	yield              func(context.Context) bool
	logger             *slog.Logger
}

func (r alarmDispatchRunner) runOnce(ctx context.Context) (bool, error) {
	envelopes, err := r.consumer.DrainBatch(ctx, r.maxBatch)
	if err != nil {
		return false, fmt.Errorf("drain alarm dispatch batch: %w", err)
	}
	if len(envelopes) == 0 {
		return false, nil
	}
	return true, r.dispatchGroups(ctx, groupAlarmDispatchEnvelopes(envelopes))
}

func (r alarmDispatchRunner) dispatchGroups(ctx context.Context, groups []alarmDispatchGroup) error {
	for _, group := range groups {
		if err := r.dispatchGroup(ctx, group); err != nil {
			return err
		}
	}
	return nil
}

func (r alarmDispatchRunner) dispatchGroup(ctx context.Context, group alarmDispatchGroup) error {
	if !r.karingEnabled {
		return r.dispatchMessageGroup(ctx, group)
	}
	return r.dispatchKaringContentListGroup(ctx, group)
}

func (r alarmDispatchRunner) dispatchMessageGroup(ctx context.Context, group alarmDispatchGroup) error {
	message, err := renderAlarmDispatchGroup(ctx, group)
	if err != nil {
		return r.persistPreSendFailure(ctx, group.envelopes, err)
	}
	if err := r.consumer.MarkSending(ctx, group.envelopes); err != nil {
		return fmt.Errorf("mark alarm dispatch sending: %w", err)
	}
	if err := r.sender.SendMessage(ctx, group.roomID, message); err != nil {
		return r.persistPostSendingFailure(ctx, group.envelopes, err)
	}
	if err := r.consumer.MarkDispatched(ctx, group.envelopes); err != nil {
		return fmt.Errorf("mark alarm dispatch sent: %w", err)
	}
	return nil
}

func (r alarmDispatchRunner) dispatchKaringContentListGroup(ctx context.Context, group alarmDispatchGroup) error {
	req, err := buildAlarmDispatchKaringContentListRequest(group)
	if err != nil {
		return r.persistPreSendFailure(ctx, group.envelopes, err)
	}
	if err := r.consumer.MarkSending(ctx, group.envelopes); err != nil {
		return fmt.Errorf("mark alarm dispatch sending: %w", err)
	}
	if err := r.sender.SendKaringContentList(ctx, group.roomID, req); err != nil {
		return r.persistPostSendingFailure(ctx, group.envelopes, err)
	}
	if err := r.consumer.MarkDispatched(ctx, group.envelopes); err != nil {
		return fmt.Errorf("mark alarm dispatch sent: %w", err)
	}
	return nil
}

func (r alarmDispatchRunner) persistPreSendFailure(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	retryEnvelopes, dlqEnvelopes := prepareDispatchFailure(envelopes, cause)
	if err := r.consumer.ScheduleRetry(ctx, retryEnvelopes); err != nil {
		return r.preserveAfterPersistenceFailure(ctx, retryEnvelopes, fmt.Errorf("schedule alarm dispatch retry: %w", err))
	}
	if err := r.consumer.MoveToDLQ(ctx, dlqEnvelopes); err != nil {
		return r.preserveAfterPersistenceFailure(ctx, dlqEnvelopes, fmt.Errorf("move alarm dispatch dlq: %w", err))
	}
	if err := r.consumer.ReleaseClaimKeys(ctx, claimKeysForAlarmDispatchEnvelopes(dlqEnvelopes)); err != nil {
		return fmt.Errorf("release alarm dispatch dlq claim keys: %w", err)
	}
	return nil
}

func (r alarmDispatchRunner) persistPostSendingFailure(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
	if !r.postSendQuarantine {
		return r.persistPreSendFailure(ctx, envelopes, cause)
	}
	consumer, ok := r.consumer.(alarmDispatchQuarantineConsumer)
	if !ok {
		return r.persistPreSendFailure(ctx, envelopes, cause)
	}
	reason := cause.Error()
	if err := consumer.Quarantine(ctx, envelopes, reason); err != nil {
		return fmt.Errorf("quarantine alarm dispatch after send failure: %w", err)
	}
	observeAlarmDispatchRunnerPostSendQuarantined(len(envelopes))
	return nil
}

func (r alarmDispatchRunner) preserveAfterPersistenceFailure(
	ctx context.Context,
	envelopes []domain.AlarmQueueEnvelope,
	persistErr error,
) error {
	if len(envelopes) == 0 {
		return persistErr
	}
	if err := r.consumer.Requeue(ctx, envelopes); err != nil {
		return fmt.Errorf("%w: fallback requeue: %w", persistErr, err)
	}
	return persistErr
}

type alarmDispatchGroup struct {
	roomID        string
	minutesUntil  int
	envelopes     []domain.AlarmQueueEnvelope
	notifications []domain.AlarmNotification
}

func groupAlarmDispatchEnvelopes(envelopes []domain.AlarmQueueEnvelope) []alarmDispatchGroup {
	groups := make([]alarmDispatchGroup, 0, len(envelopes))
	index := map[string]int{}
	for _, envelope := range envelopes {
		key := alarmDispatchGroupKey(envelope)
		groupIndex, ok := index[key]
		if !ok {
			index[key] = len(groups)
			groups = append(groups, newAlarmDispatchGroup(envelope))
			continue
		}
		appendAlarmDispatchEnvelope(&groups[groupIndex], envelope)
	}
	return groups
}

func newAlarmDispatchGroup(envelope domain.AlarmQueueEnvelope) alarmDispatchGroup {
	return alarmDispatchGroup{
		roomID:        envelope.Notification.RoomID,
		minutesUntil:  envelope.Notification.MinutesUntil,
		envelopes:     []domain.AlarmQueueEnvelope{envelope},
		notifications: []domain.AlarmNotification{envelope.Notification},
	}
}

func appendAlarmDispatchEnvelope(group *alarmDispatchGroup, envelope domain.AlarmQueueEnvelope) {
	group.minutesUntil = minAlarmDispatchMinutes(group.minutesUntil, envelope.Notification.MinutesUntil)
	group.envelopes = append(group.envelopes, envelope)
	group.notifications = append(group.notifications, envelope.Notification)
}

func alarmDispatchGroupKey(envelope domain.AlarmQueueEnvelope) string {
	if envelope.SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox && envelope.YouTubeOutbox != nil {
		return fmt.Sprintf("%s|source|%s|%s|%s|%s",
			envelope.Notification.RoomID,
			envelope.SourceKind,
			envelope.YouTubeOutbox.ChannelID,
			envelope.YouTubeOutbox.Kind,
			envelope.YouTubeOutbox.Identity(),
		)
	}
	if envelope.Notification.Stream != nil && envelope.Notification.Stream.StartScheduled != nil {
		minuteBucket := envelope.Notification.Stream.StartScheduled.UTC().Unix() / 60
		return fmt.Sprintf("%s|scheduled|%d", envelope.Notification.RoomID, minuteBucket)
	}
	return fmt.Sprintf("%s|minutes|%d", envelope.Notification.RoomID, envelope.Notification.MinutesUntil)
}

func minAlarmDispatchMinutes(current, next int) int {
	if next < 0 {
		return current
	}
	if current < 0 || next < current {
		return next
	}
	return current
}

func renderAlarmDispatchGroup(ctx context.Context, group alarmDispatchGroup) (string, error) {
	if len(group.envelopes) > 0 && group.envelopes[0].SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox {
		return renderAlarmDispatchYouTubeOutbox(ctx, group.envelopes[0])
	}
	if len(group.notifications) == 1 {
		return renderAlarmDispatchNotification(group.notifications[0]), nil
	}
	return renderAlarmDispatchNotificationGroup(group), nil
}

func renderAlarmDispatchYouTubeOutbox(ctx context.Context, envelope domain.AlarmQueueEnvelope) (string, error) {
	if envelope.YouTubeOutbox == nil {
		return "", fmt.Errorf("render youtube outbox dispatch: payload is nil")
	}
	return outbox.FormatYouTubeOutboxPayload(ctx, *envelope.YouTubeOutbox)
}

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

func alarmDispatchOutboxKaringLabels(kind domain.OutboxKind) (string, string) {
	switch kind {
	case domain.OutboxKindCommunityPost:
		return "커뮤니티 알림", "새 커뮤니티"
	case domain.OutboxKindNewShort:
		return "쇼츠 알림", "새 쇼츠"
	case domain.OutboxKindNewVideo:
		return "새 영상", "새 영상"
	case domain.OutboxKindLiveStream:
		return "방송 알림", "방송 알림"
	default:
		return "알림", "새 알림"
	}
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

func alarmDispatchVideoOutboxStatus(kind domain.OutboxKind, data alarmDispatchKaringVideoPayload) string {
	switch kind {
	case domain.OutboxKindNewShort:
		return "쇼츠"
	case domain.OutboxKindNewVideo:
		return "새 영상"
	case domain.OutboxKindLiveStream:
		if data.PublishedAt == nil {
			return string(iris.KaringStreamStatusUpcoming)
		}
		return string(iris.KaringStreamStatusLive)
	default:
		return "알림"
	}
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

func renderAlarmDispatchNotificationGroup(group alarmDispatchGroup) string {
	var builder strings.Builder
	if group.minutesUntil <= 0 {
		builder.WriteString("🔔 방송 시작 알림")
	} else {
		fmt.Fprintf(&builder, "⏰ 방송 %d분 전 알림", group.minutesUntil)
	}
	for _, notification := range group.notifications {
		builder.WriteString("\n\n")
		builder.WriteString(renderAlarmDispatchNotificationInGroup(notification, group.minutesUntil))
	}
	return builder.String()
}

func renderAlarmDispatchNotification(notification domain.AlarmNotification) string {
	return renderAlarmDispatchNotificationInGroup(notification, -1)
}

func renderAlarmDispatchNotificationInGroup(notification domain.AlarmNotification, groupMinutesUntil int) string {
	memberName := resolveAlarmDispatchMemberName(notification)
	title := resolveAlarmDispatchTitle(notification)
	url := resolveAlarmDispatchURL(notification)
	var builder strings.Builder
	if notification.MinutesUntil <= 0 {
		fmt.Fprintf(&builder, "🔔 %s 방송 시작!\n", memberName)
	} else if groupMinutesUntil > 0 && notification.MinutesUntil == groupMinutesUntil {
		fmt.Fprintf(&builder, "⏰ %s 방송 예정\n", memberName)
	} else {
		fmt.Fprintf(&builder, "⏰ %s 방송 %d분 전\n", memberName, notification.MinutesUntil)
	}
	fmt.Fprintf(&builder, "📺 %s\n", title)
	if scheduleMessage := strings.TrimSpace(notification.ScheduleChangeMessage); scheduleMessage != "" {
		fmt.Fprintf(&builder, "📅 %s\n", scheduleMessage)
	}
	if url != "" {
		fmt.Fprintf(&builder, "🔗 %s", url)
	}
	return strings.TrimSpace(builder.String())
}

func resolveAlarmDispatchMemberName(notification domain.AlarmNotification) string {
	if notification.Channel != nil && strings.TrimSpace(notification.Channel.Name) != "" {
		return strings.TrimSpace(notification.Channel.Name)
	}
	if notification.Stream != nil && strings.TrimSpace(notification.Stream.ChannelName) != "" {
		return strings.TrimSpace(notification.Stream.ChannelName)
	}
	return "알 수 없는 멤버"
}

func resolveAlarmDispatchTitle(notification domain.AlarmNotification) string {
	if notification.Stream == nil {
		return "방송 정보 없음"
	}
	if title := strings.TrimSpace(notification.Stream.Title); title != "" {
		return title
	}
	return "제목 없음"
}

func resolveAlarmDispatchURL(notification domain.AlarmNotification) string {
	if notification.Stream == nil {
		return ""
	}
	stream := notification.Stream
	if url, ok := resolveAlarmDispatchDirectPlatformURL(stream); ok {
		return url
	}
	if stream.IsIntegrated {
		return resolveAlarmDispatchIntegratedURL(stream)
	}
	return stream.GetYouTubeURL()
}

func resolveAlarmDispatchDirectPlatformURL(stream *domain.Stream) (string, bool) {
	if stream.IsTwitchOnly && stream.GetTwitchLiveURL() != "" {
		return stream.GetTwitchLiveURL(), true
	}
	if stream.IsChzzkOnly && stream.GetChzzkLiveURL() != "" {
		return stream.GetChzzkLiveURL(), true
	}
	return "", false
}

func resolveAlarmDispatchIntegratedURL(stream *domain.Stream) string {
	youtubeURL := stream.GetYouTubeURL()
	if youtubeURL == "" {
		return ""
	}
	if chzzkURL := stream.GetChzzkLiveURL(); chzzkURL != "" {
		return fmt.Sprintf("%s | %s", youtubeURL, chzzkURL)
	}
	return youtubeURL
}

func claimKeysForAlarmDispatchEnvelopes(envelopes []domain.AlarmQueueEnvelope) []string {
	claimKeys := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		claimKeys = append(claimKeys, envelope.ClaimKeys...)
	}
	return claimKeys
}

func prepareDispatchFailure(envelopes []domain.AlarmQueueEnvelope, cause error) ([]domain.AlarmQueueEnvelope, []domain.AlarmQueueEnvelope) {
	retryEnvelopes := make([]domain.AlarmQueueEnvelope, 0, len(envelopes))
	dlqEnvelopes := make([]domain.AlarmQueueEnvelope, 0, len(envelopes))
	for _, envelope := range envelopes {
		updated := envelope
		updated.Retry = nextAlarmDispatchRetry(envelope, cause)
		if updated.Retry.Attempt >= 3 {
			dlqEnvelopes = append(dlqEnvelopes, updated)
			continue
		}
		retryEnvelopes = append(retryEnvelopes, updated)
	}
	return retryEnvelopes, dlqEnvelopes
}

func nextAlarmDispatchRetry(envelope domain.AlarmQueueEnvelope, cause error) *domain.AlarmQueueRetryMetadata {
	retry := &domain.AlarmQueueRetryMetadata{}
	if envelope.Retry != nil {
		*retry = *envelope.Retry
	}
	retry.Attempt++
	retry.LastError = cause.Error()
	retry.RetryAfterMS = int64((time.Duration(retry.Attempt) * 5 * time.Second) / time.Millisecond)
	retry.NextVisibleAt = time.Now().UTC().Add(time.Duration(retry.RetryAfterMS) * time.Millisecond).Format(time.RFC3339Nano)
	return retry
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
