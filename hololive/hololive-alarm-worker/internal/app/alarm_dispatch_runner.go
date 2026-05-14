package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
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

type alarmDispatchSender interface {
	SendMessage(ctx context.Context, roomID, message string) error
}

type alarmDispatchRunner struct {
	consumer alarmDispatchConsumer
	sender   alarmDispatchSender
	maxBatch int
	logger   *slog.Logger
}

func (r alarmDispatchRunner) Start(ctx context.Context) error {
	if r.consumer == nil || r.sender == nil {
		return nil
	}
	for {
		if !r.runStep(ctx) {
			return nil
		}
	}
}

func (r alarmDispatchRunner) runStep(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	processed, err := r.runOnce(ctx)
	if err != nil {
		return r.handleStepError(ctx, err)
	}
	if !processed {
		return sleepContext(ctx, 25*time.Millisecond)
	}
	return true
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
	message, err := renderAlarmDispatchGroup(ctx, group)
	if err != nil {
		return r.persistDispatchFailure(ctx, group.envelopes, err)
	}
	if err := r.consumer.MarkSending(ctx, group.envelopes); err != nil {
		return fmt.Errorf("mark alarm dispatch sending: %w", err)
	}
	if err := r.sender.SendMessage(ctx, group.roomID, message); err != nil {
		return r.persistDispatchFailure(ctx, group.envelopes, err)
	}
	if err := r.consumer.MarkDispatched(ctx, group.envelopes); err != nil {
		return fmt.Errorf("mark alarm dispatch sent: %w", err)
	}
	return nil
}

func (r alarmDispatchRunner) persistDispatchFailure(ctx context.Context, envelopes []domain.AlarmQueueEnvelope, cause error) error {
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

func (r alarmDispatchRunner) handleStepError(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	if r.logger != nil {
		r.logger.Warn("Alarm dispatch loop iteration failed", slog.Any("error", err))
	}
	return sleepContext(ctx, time.Second)
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
