package youtubedispatch

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v6/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

var errFinalResultSendFailed = errors.Join(errors.New(testSendFailedMessage), iris.ErrPermanent)

type finalResultTestSender struct {
	failRoom map[string]bool
}

func (s *finalResultTestSender) SendMessage(_ context.Context, roomID, _ string) error {
	if s.failRoom[roomID] {
		return errFinalResultSendFailed
	}

	return nil
}

type finalResultOutboxModel struct {
	ID            int64     `db:"id"`
	Kind          string    `db:"kind"`
	ChannelID     string    `db:"channel_id"`
	ContentID     string    `db:"content_id"`
	Payload       string    `db:"payload"`
	Status        string    `db:"status"`
	AttemptCount  int       `db:"attempt_count"`
	NextAttemptAt time.Time `db:"next_attempt_at"`
	CreatedAt     time.Time
	LockedAt      *time.Time
	SentAt        *time.Time
	Error         string `db:"error"`
}

func (finalResultOutboxModel) TableName() string {
	return testTableOutbox
}

type finalResultDeliveryModel struct {
	ID            int64     `db:"id"`
	OutboxID      int64     `db:"outbox_id"`
	RoomID        string    `db:"room_id"`
	Status        string    `db:"status"`
	AttemptCount  int       `db:"attempt_count"`
	NextAttemptAt time.Time `db:"next_attempt_at"`
	CreatedAt     time.Time
	LockedAt      *time.Time
	SentAt        *time.Time
	Error         string `db:"error"`
}

func (finalResultDeliveryModel) TableName() string {
	return testTableDelivery
}

type finalResultTrackingModel struct {
	Kind                        string `db:"kind"`
	ContentID                   string `db:"content_id"`
	CanonicalContentID          string
	ChannelID                   string `db:"channel_id"`
	ActualPublishedAt           *time.Time
	DetectedAt                  time.Time `db:"detected_at"`
	AlarmSentAt                 *time.Time
	AlarmLatencyMillis          *int64
	AlarmLatencyExceeded        *bool
	DeliveryStatus              string `db:"delivery_status"`
	LatencyClassificationStatus string
	DelaySource                 string
	InternalDelayCause          string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

func (finalResultTrackingModel) TableName() string {
	return testTableContentAlarmTracking
}

func TestProcessPendingDeliveries_LogsCommunityShortsFinalSuccessResult(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	now := time.Now().UTC().Truncate(time.Microsecond)
	actualPublishedAt := now.Add(-190 * time.Second)
	detectedAt := now.Add(-150 * time.Second)
	item := finalResultOutboxModel{
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_final_success",
		ContentID:     "short-final-success",
		Payload:       `{"canonical_post_id":"short:short-final-success","video_id":"short-final-success","title":"short title"}`,
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &item).Error)
	require.NoError(t, insertDeliveryTestRows(db, &finalResultTrackingModel{
		Kind:              string(domain.OutboxKindNewShort),
		ContentID:         item.ContentID,
		ChannelID:         item.ChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}).Error)
	require.NoError(t, insertDeliveryTestRows(db, &finalResultDeliveryModel{
		OutboxID:      item.ID,
		RoomID:        "room-success",
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}).Error)

	dispatcher, logBuffer := newLoggedSQLiteDispatcherForFinalResultTest(t, db, &finalResultTestSender{failRoom: map[string]bool{}}, &Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.claim.processPendingDeliveries(ctx)

	entry := findOutboxFinalResultAuditLogEntry(t, logBuffer)
	assertLogStringField(t, entry, FieldContentID, "short-final-success")
	assertLogStringField(t, entry, FieldPostID, "short:short-final-success")
	assertLogStringField(t, entry, FieldAlarmType, string(domain.AlarmTypeShorts))
	assertLogStringField(t, entry, FieldSendResult, sendResultSuccess)
	assertLogStringField(t, entry, FieldDeliveryMode, DeliveryModeFinalResult)
	assertLogTimeField(t, entry, FieldActualPublishedAt, actualPublishedAt)
	assertLogStringField(t, entry, FieldDedupeKey, "youtube-notification:NEW_SHORT:short-final-success")
	assertLogTimeField(t, entry, FieldAlarmSentAt)
	assertLogBoolField(t, entry, FieldAlarmLatencyExceeded, true)
	require.GreaterOrEqual(t, readLogIntField(t, entry, FieldAlarmLatencyMillis), 190000)
	assertLogIntField(t, entry, FieldTargetRoomCount, 1)
	assertLogIntField(t, entry, FieldSuccessfulRoomCount, 1)
	assertLogIntField(t, entry, FieldFailedRoomCount, 0)
	assertLogTimeField(t, entry, FieldSentAt)
}

func TestProcessPendingDeliveries_FinalResultReadsTrackingByCanonicalID(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	now := time.Now().UTC().Truncate(time.Microsecond)
	actualPublishedAt := now.Add(-190 * time.Second)
	item := finalResultOutboxModel{
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_final_alias",
		ContentID:     "short:short-final-alias",
		Payload:       `{"canonical_post_id":"short:short-final-alias","video_id":"short-final-alias","title":"short title"}`,
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &item).Error)
	require.NoError(t, insertDeliveryTestRows(db, &finalResultTrackingModel{
		Kind:              string(domain.OutboxKindNewShort),
		ContentID:         "short-final-alias",
		ChannelID:         item.ChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        now.Add(-150 * time.Second),
	}).Error)
	require.NoError(t, insertDeliveryTestRows(db, &finalResultDeliveryModel{
		OutboxID:      item.ID,
		RoomID:        "room-alias",
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}).Error)

	dispatcher, logBuffer := newLoggedSQLiteDispatcherForFinalResultTest(t, db, &finalResultTestSender{failRoom: map[string]bool{}}, &Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.claim.processPendingDeliveries(ctx)

	entry := findOutboxFinalResultAuditLogEntry(t, logBuffer)
	assertLogStringField(t, entry, FieldSendResult, sendResultSuccess)
	assertLogTimeField(t, entry, FieldActualPublishedAt, actualPublishedAt)
	assertLogBoolField(t, entry, FieldAlarmLatencyExceeded, true)
	require.GreaterOrEqual(t, readLogIntField(t, entry, FieldAlarmLatencyMillis), 190000)
}

func TestProcessPendingDeliveries_FinalResultReadsCommunityTrackingThroughPostURL(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	now := time.Now().UTC().Truncate(time.Microsecond)
	actualPublishedAt := now.Add(-200 * time.Second)
	item := finalResultOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_final_community_url",
		ContentID:     "https://www.youtube.com/post/UgkxCommunityURL",
		Payload:       `{"canonical_post_id":"community:UgkxCommunityURL","post_id":"UgkxCommunityURL","content_text":"community title"}`,
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &item).Error)
	require.NoError(t, insertDeliveryTestRows(db, &finalResultTrackingModel{
		Kind:              string(domain.OutboxKindCommunityPost),
		ContentID:         "UgkxCommunityURL",
		ChannelID:         item.ChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        now.Add(-150 * time.Second),
	}).Error)
	require.NoError(t, insertDeliveryTestRows(db, &finalResultDeliveryModel{
		OutboxID:      item.ID,
		RoomID:        "room-community-url",
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}).Error)

	dispatcher, logBuffer := newLoggedSQLiteDispatcherForFinalResultTest(t, db, &finalResultTestSender{failRoom: map[string]bool{}}, &Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.claim.processPendingDeliveries(ctx)

	entry := findOutboxFinalResultAuditLogEntry(t, logBuffer)
	assertLogStringField(t, entry, FieldPostID, "community:UgkxCommunityURL")
	assertLogStringField(t, entry, FieldSendResult, sendResultSuccess)
	assertLogTimeField(t, entry, FieldActualPublishedAt, actualPublishedAt)
	require.GreaterOrEqual(t, readLogIntField(t, entry, FieldAlarmLatencyMillis), 200000)
}

func TestProcessPendingDeliveries_FinalResultWithoutTrackingUsesOutboxSentAt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	now := time.Now().UTC().Truncate(time.Microsecond)
	item := finalResultOutboxModel{
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_final_untracked",
		ContentID:     "short-final-untracked",
		Payload:       `{"canonical_post_id":"short:short-final-untracked","video_id":"short-final-untracked","title":"short title"}`,
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &item).Error)
	require.NoError(t, insertDeliveryTestRows(db, &finalResultDeliveryModel{
		OutboxID:      item.ID,
		RoomID:        "room-untracked",
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}).Error)

	dispatcher, logBuffer := newLoggedSQLiteDispatcherForFinalResultTest(t, db, &finalResultTestSender{failRoom: map[string]bool{}}, &Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.claim.processPendingDeliveries(ctx)

	entry := findOutboxFinalResultAuditLogEntry(t, logBuffer)
	assertLogStringField(t, entry, FieldSendResult, sendResultSuccess)
	require.NotContains(t, entry, FieldActualPublishedAt)
	require.NotContains(t, entry, FieldAlarmLatencyMillis)
	require.Equal(t, readLogTimeField(t, entry, FieldSentAt), readLogTimeField(t, entry, FieldAlarmSentAt))
}

func TestProcessPendingDeliveries_FinalResultSurvivesTrackingLookupFailure(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	now := time.Now().UTC().Truncate(time.Microsecond)
	actualPublishedAt := now.Add(-190 * time.Second)
	item := finalResultOutboxModel{
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_final_tracking_down",
		ContentID:     "short-final-tracking-down",
		Payload:       `{"canonical_post_id":"short:short-final-tracking-down","video_id":"short-final-tracking-down","title":"short title"}`,
		Status:        string(domain.OutboxStatusSent),
		NextAttemptAt: now,
		SentAt:        &now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &item).Error)
	require.NoError(t, insertDeliveryTestRows(db, &finalResultTrackingModel{
		Kind:              string(domain.OutboxKindNewShort),
		ContentID:         item.ContentID,
		ChannelID:         item.ChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        now.Add(-150 * time.Second),
	}).Error)
	require.NoError(t, insertDeliveryTestRows(db, &finalResultDeliveryModel{
		OutboxID:      item.ID,
		RoomID:        "room-tracking-down",
		Status:        string(domain.OutboxStatusSent),
		NextAttemptAt: now,
		SentAt:        &now,
	}).Error)

	dispatcher, logBuffer := newLoggedSQLiteDispatcherForFinalResultTest(t, db, &finalResultTestSender{failRoom: map[string]bool{}}, &Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	_, err := db.Exec(ctx, `ALTER TABLE youtube_content_alarm_tracking RENAME TO youtube_content_alarm_tracking_unavailable`)
	require.NoError(t, err)

	require.NoError(t, dispatcher.claim.logFinalizedCommunityShortsOutboxResults(ctx, []int64{item.ID}))

	entry := findOutboxFinalResultAuditLogEntry(t, logBuffer)
	assertLogStringField(t, entry, FieldSendResult, sendResultSuccess)
	assertLogIntField(t, entry, FieldSuccessfulRoomCount, 1)
	require.NotContains(t, entry, FieldActualPublishedAt)
	require.NotContains(t, entry, FieldAlarmSentAt)
	require.NotContains(t, entry, FieldAlarmLatencyMillis)
	require.NotContains(t, entry, FieldAlarmLatencyExceeded)

	warnings := findAllLogEntriesByMessage(t, logBuffer, CommunityShortsFinalResultTrackingUnavailableMessage)
	require.Len(t, warnings, 1)
	require.Equal(t, []any{float64(item.ID)}, warnings[0]["outbox_ids"])
}

func TestProcessPendingDeliveries_LogsCommunityShortsFinalCommunitySuccessResult(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	now := time.Now().UTC().Truncate(time.Microsecond)
	actualPublishedAt := now.Add(-190 * time.Second)
	detectedAt := now.Add(-150 * time.Second)
	item := finalResultOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_final_community_success",
		ContentID:     "post-final-community-success",
		Payload:       `{"canonical_post_id":"community:post-final-community-success","post_id":"post-resource","content_text":"community title"}`,
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &item).Error)
	require.NoError(t, insertDeliveryTestRows(db, &finalResultTrackingModel{
		Kind:              string(domain.OutboxKindCommunityPost),
		ContentID:         item.ContentID,
		ChannelID:         item.ChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}).Error)
	require.NoError(t, insertDeliveryTestRows(db, &finalResultDeliveryModel{
		OutboxID:      item.ID,
		RoomID:        "room-community-success",
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}).Error)

	dispatcher, logBuffer := newLoggedSQLiteDispatcherForFinalResultTest(t, db, &finalResultTestSender{failRoom: map[string]bool{}}, &Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.claim.processPendingDeliveries(ctx)

	entry := findOutboxFinalResultAuditLogEntry(t, logBuffer)
	assertLogStringField(t, entry, FieldContentID, "post-final-community-success")
	assertLogStringField(t, entry, FieldPostID, "community:post-final-community-success")
	assertLogStringField(t, entry, FieldAlarmType, string(domain.AlarmTypeCommunity))
	assertLogStringField(t, entry, FieldSendResult, sendResultSuccess)
	assertLogStringField(t, entry, FieldDeliveryMode, DeliveryModeFinalResult)
	assertLogTimeField(t, entry, FieldActualPublishedAt, actualPublishedAt)
	assertLogStringField(t, entry, FieldDedupeKey, "youtube-notification:COMMUNITY_POST:post-final-community-success")
	assertLogTimeField(t, entry, FieldAlarmSentAt)
	require.GreaterOrEqual(t, readLogIntField(t, entry, FieldAlarmLatencyMillis), 190000)
	assertLogIntField(t, entry, FieldTargetRoomCount, 1)
	assertLogIntField(t, entry, FieldSuccessfulRoomCount, 1)
	assertLogIntField(t, entry, FieldFailedRoomCount, 0)
	assertLogTimeField(t, entry, FieldSentAt)
}

func TestProcessPendingDeliveries_LogsCommunityShortsFinalFailureReason(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	now := time.Now().UTC().Truncate(time.Microsecond)
	actualPublishedAt := now.Add(-200 * time.Second)
	detectedAt := now.Add(-160 * time.Second)
	item := finalResultOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_final_failure",
		ContentID:     "post-final-failure",
		Payload:       `{"canonical_post_id":"community:post-final-failure","post_id":"post-resource","content_text":"community title"}`,
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}
	require.NoError(t, insertDeliveryTestRows(db, &item).Error)
	require.NoError(t, insertDeliveryTestRows(db, &finalResultTrackingModel{
		Kind:              string(domain.OutboxKindCommunityPost),
		ContentID:         item.ContentID,
		ChannelID:         item.ChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}).Error)
	require.NoError(t, insertDeliveryTestRows(db, &finalResultDeliveryModel{
		OutboxID:      item.ID,
		RoomID:        "room-failure",
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}).Error)

	dispatcher, logBuffer := newLoggedSQLiteDispatcherForFinalResultTest(t, db, &finalResultTestSender{failRoom: map[string]bool{"room-failure": true}}, &Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          1,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.claim.processPendingDeliveries(ctx)

	entry := findOutboxFinalResultAuditLogEntry(t, logBuffer)
	assertLogStringField(t, entry, FieldContentID, "post-final-failure")
	assertLogStringField(t, entry, FieldPostID, "community:post-final-failure")
	assertLogStringField(t, entry, FieldAlarmType, string(domain.AlarmTypeCommunity))
	assertLogStringField(t, entry, FieldSendResult, sendResultFailure)
	assertLogStringField(t, entry, FieldFailureReason, string(lifecycleReasonPermanent))
	assertLogStringField(t, entry, FieldDeliveryMode, DeliveryModeFinalResult)
	assertLogTimeField(t, entry, FieldActualPublishedAt, actualPublishedAt)
	assertLogIntField(t, entry, FieldTargetRoomCount, 1)
	assertLogIntField(t, entry, FieldSuccessfulRoomCount, 0)
	assertLogIntField(t, entry, FieldFailedRoomCount, 1)
	assertLogTimeField(t, entry, FieldSentAt)
}

func newLoggedSQLiteDispatcherForFinalResultTest(t *testing.T, db *pgxpool.Pool, sender *finalResultTestSender, config *Config) (*Dispatcher, *bytes.Buffer) {
	t.Helper()

	logBuffer := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cache := cachemocks.NewLenientClient()

	return newDispatcherForTest(t, db, cache, sender, nil, logger, config), logBuffer
}

func findOutboxFinalResultAuditLogEntry(t *testing.T, logBuffer *bytes.Buffer) map[string]any {
	t.Helper()

	entries := findAllLogEntriesByMessage(t, logBuffer, CommunityShortsDeliveryAuditMessage)
	for i := range entries {
		raw, ok := entries[i][FieldTelemetrySource]
		if !ok {
			continue
		}

		value, ok := raw.(string)
		if !ok {
			t.Fatalf("telemetry_source type = %T, want string", raw)
		}

		if value == TelemetrySourceOutboxFinalResult {
			return entries[i]
		}
	}

	t.Fatalf("audit log with telemetry_source=%q not found in %s", TelemetrySourceOutboxFinalResult, logBuffer.String())

	return nil
}

func findAllLogEntriesByMessage(t *testing.T, logBuffer *bytes.Buffer, message string) []map[string]any {
	t.Helper()

	entries := make([]map[string]any, 0)

	for line := range bytes.SplitSeq(bytes.TrimSpace(logBuffer.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}

		entry := make(map[string]any)
		if err := jsonv2.Unmarshal(line, &entry); err != nil {
			t.Fatalf("unmarshal log entry: %v", err)
		}

		if entry["msg"] == message {
			entries = append(entries, entry)
		}
	}

	return entries
}

func readLogStringField(t *testing.T, entry map[string]any, field string) string {
	t.Helper()

	raw, ok := entry[field]
	if !ok {
		t.Fatalf("log entry missing %q: %#v", field, entry)
	}

	value, ok := raw.(string)
	if !ok {
		t.Fatalf("log field %q type = %T, want string", field, raw)
	}

	return value
}

func readLogTimeField(t *testing.T, entry map[string]any, field string) time.Time {
	t.Helper()

	value := readLogStringField(t, entry, field)

	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("log field %q = %q, want RFC3339Nano time: %v", field, value, err)
	}

	return parsed.UTC()
}

func readLogBoolField(t *testing.T, entry map[string]any, field string) bool {
	t.Helper()

	raw, ok := entry[field]
	if !ok {
		t.Fatalf("log entry missing %q: %#v", field, entry)
	}

	value, ok := raw.(bool)
	if !ok {
		t.Fatalf("log field %q type = %T, want bool", field, raw)
	}

	return value
}

func readLogIntField(t *testing.T, entry map[string]any, field string) int {
	t.Helper()

	raw, ok := entry[field]
	if !ok {
		t.Fatalf("log entry missing %q: %#v", field, entry)
	}

	switch value := raw.(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		t.Fatalf("log field %q type = %T, want number", field, raw)
	}

	return 0
}

func assertLogStringField(t *testing.T, entry map[string]any, field, want string) {
	t.Helper()

	if got := readLogStringField(t, entry, field); got != want {
		t.Fatalf("log field %q = %q, want %q", field, got, want)
	}
}

func assertLogTimeField(t *testing.T, entry map[string]any, field string, want ...time.Time) {
	t.Helper()

	got := readLogTimeField(t, entry, field)
	if len(want) > 0 && !got.Equal(want[0].UTC()) {
		t.Fatalf("log field %q = %s, want %s", field, got.Format(time.RFC3339Nano), want[0].UTC().Format(time.RFC3339Nano))
	}
}

func assertLogBoolField(t *testing.T, entry map[string]any, field string, want bool) {
	t.Helper()

	if got := readLogBoolField(t, entry, field); got != want {
		t.Fatalf("log field %q = %t, want %t", field, got, want)
	}
}

func assertLogIntField(t *testing.T, entry map[string]any, field string, want int) {
	t.Helper()

	if got := readLogIntField(t, entry, field); got != want {
		t.Fatalf("log field %q = %d, want %d", field, got, want)
	}
}

func (s *finalResultTestSender) PrepareMessageRequest(_ context.Context, _ string, body delivery.MessageVariants) (string, string, error) {
	return body.Plain, testPreparedTextRoute, nil
}

func (s *finalResultTestSender) SendPreparedMessage(ctx context.Context, room, body, _, id string) (string, error) {
	return testAdmission(id, s.SendMessage(ctx, room, body))
}
