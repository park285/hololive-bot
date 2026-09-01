package youtubedispatch

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/park285/iris-client-go/v2/iris"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	dispatchstate "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
	timeline "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/timeline"
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

	dispatcher, logBuffer := newLoggedSQLiteDispatcherForFinalResultTest(t, db, &finalResultTestSender{failRoom: map[string]bool{}}, &dispatchstate.Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.claim.processPendingDeliveries(ctx)

	entry := findOutboxFinalResultAuditLogEntry(t, logBuffer)
	assertLogStringField(t, entry, deliveryAuditContentIDLogField, "short-final-success")
	assertLogStringField(t, entry, deliveryAuditPostIDLogField, "short:short-final-success")
	assertLogStringField(t, entry, deliveryAuditAlarmTypeLogField, string(domain.AlarmTypeShorts))
	assertLogStringField(t, entry, deliveryAuditSendResultLogField, sendResultSuccess)
	assertLogStringField(t, entry, deliveryAuditModeLogField, logschema.DeliveryModeFinalResult)
	assertLogTimeField(t, entry, logschema.FieldActualPublishedAt, actualPublishedAt)
	assertLogStringField(t, entry, deliveryDedupeKeyLogField, "youtube-notification:NEW_SHORT:short-final-success")
	assertLogTimeField(t, entry, logschema.FieldAlarmSentAt)
	assertLogBoolField(t, entry, logschema.FieldAlarmLatencyExceeded, true)
	require.GreaterOrEqual(t, readLogIntField(t, entry, logschema.FieldAlarmLatencyMillis), 190000)
	assertLogIntField(t, entry, logschema.FieldTargetRoomCount, 1)
	assertLogIntField(t, entry, logschema.FieldSuccessfulRoomCount, 1)
	assertLogIntField(t, entry, logschema.FieldFailedRoomCount, 0)
	assertLogTimeField(t, entry, deliveryAuditSentAtLogField)

	classification := readLatencyClassificationField(t, entry)
	assertLogObjectStringField(t, classification, "status", string(timeline.PostLatencyClassificationStatusExceeded))
	assertLogObjectIntField(t, classification, "threshold_millis", int(timeline.PostLatencyExceededThresholdMillis))
	assertLogObjectStringField(t, classification, "delay_source", string(timeline.PostDelaySourceInternalDelivery))
	assertLogObjectStringField(t, classification, "internal_delay_cause", string(timeline.PostInternalDelayCauseQueueWait))
	assertLogObjectStringField(t, classification, "reason_code", string(timeline.PostLatencyReasonCodeQueueWait))
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

	dispatcher, logBuffer := newLoggedSQLiteDispatcherForFinalResultTest(t, db, &finalResultTestSender{failRoom: map[string]bool{}}, &dispatchstate.Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.claim.processPendingDeliveries(ctx)

	entry := findOutboxFinalResultAuditLogEntry(t, logBuffer)
	assertLogStringField(t, entry, deliveryAuditContentIDLogField, "post-final-community-success")
	assertLogStringField(t, entry, deliveryAuditPostIDLogField, "community:post-final-community-success")
	assertLogStringField(t, entry, deliveryAuditAlarmTypeLogField, string(domain.AlarmTypeCommunity))
	assertLogStringField(t, entry, deliveryAuditSendResultLogField, sendResultSuccess)
	assertLogStringField(t, entry, deliveryAuditModeLogField, logschema.DeliveryModeFinalResult)
	assertLogTimeField(t, entry, logschema.FieldActualPublishedAt, actualPublishedAt)
	assertLogStringField(t, entry, deliveryDedupeKeyLogField, "youtube-notification:COMMUNITY_POST:post-final-community-success")
	assertLogTimeField(t, entry, logschema.FieldAlarmSentAt)
	require.GreaterOrEqual(t, readLogIntField(t, entry, logschema.FieldAlarmLatencyMillis), 190000)
	assertLogIntField(t, entry, logschema.FieldTargetRoomCount, 1)
	assertLogIntField(t, entry, logschema.FieldSuccessfulRoomCount, 1)
	assertLogIntField(t, entry, logschema.FieldFailedRoomCount, 0)
	assertLogTimeField(t, entry, deliveryAuditSentAtLogField)

	classification := readLatencyClassificationField(t, entry)
	assertLogObjectStringField(t, classification, "status", string(timeline.PostLatencyClassificationStatusExceeded))
	assertLogObjectIntField(t, classification, "threshold_millis", int(timeline.PostLatencyExceededThresholdMillis))
	assertLogObjectStringField(t, classification, "delay_source", string(timeline.PostDelaySourceInternalDelivery))
	assertLogObjectStringField(t, classification, "internal_delay_cause", string(timeline.PostInternalDelayCauseQueueWait))
	assertLogObjectStringField(t, classification, "reason_code", string(timeline.PostLatencyReasonCodeQueueWait))
}

func TestProcessPendingDeliveries_LogsCommunityShortsFinalExternalDelayReasonCode(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := newDeliveryPool(t)

	now := time.Now().UTC().Truncate(time.Microsecond)
	actualPublishedAt := now.Add(-210 * time.Second)
	detectedAt := now.Add(-15 * time.Second)
	item := finalResultOutboxModel{
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_final_external_delay",
		ContentID:     "short-final-external-delay",
		Payload:       `{"canonical_post_id":"short:short-final-external-delay","video_id":"short-final-external-delay","title":"short title"}`,
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
		RoomID:        "room-external-delay",
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}).Error)

	dispatcher, logBuffer := newLoggedSQLiteDispatcherForFinalResultTest(t, db, &finalResultTestSender{failRoom: map[string]bool{}}, &dispatchstate.Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.claim.processPendingDeliveries(ctx)

	entry := findOutboxFinalResultAuditLogEntry(t, logBuffer)
	classification := readLatencyClassificationField(t, entry)
	assertLogObjectStringField(t, classification, "status", string(timeline.PostLatencyClassificationStatusExceeded))
	assertLogObjectStringField(t, classification, "delay_source", string(timeline.PostDelaySourceExternalCollection))
	assertLogObjectStringField(t, classification, "internal_delay_cause", string(timeline.PostInternalDelayCauseQueueWait))
	assertLogObjectStringField(t, classification, "reason_code", string(timeline.PostLatencyReasonCodeExternalCollection))
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

	dispatcher, logBuffer := newLoggedSQLiteDispatcherForFinalResultTest(t, db, &finalResultTestSender{failRoom: map[string]bool{"room-failure": true}}, &dispatchstate.Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          1,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.claim.processPendingDeliveries(ctx)

	entry := findOutboxFinalResultAuditLogEntry(t, logBuffer)
	assertLogStringField(t, entry, deliveryAuditContentIDLogField, "post-final-failure")
	assertLogStringField(t, entry, deliveryAuditPostIDLogField, "community:post-final-failure")
	assertLogStringField(t, entry, deliveryAuditAlarmTypeLogField, string(domain.AlarmTypeCommunity))
	assertLogStringField(t, entry, deliveryAuditSendResultLogField, sendResultFailure)
	assertLogStringField(t, entry, deliveryAuditFailureReasonLogField, string(lifecycleReasonPermanent))
	assertLogStringField(t, entry, deliveryAuditModeLogField, logschema.DeliveryModeFinalResult)
	assertLogTimeField(t, entry, logschema.FieldActualPublishedAt, actualPublishedAt)
	assertLogIntField(t, entry, logschema.FieldTargetRoomCount, 1)
	assertLogIntField(t, entry, logschema.FieldSuccessfulRoomCount, 0)
	assertLogIntField(t, entry, logschema.FieldFailedRoomCount, 1)
	assertLogTimeField(t, entry, deliveryAuditSentAtLogField)

	classification := readLatencyClassificationField(t, entry)
	assertLogObjectStringField(t, classification, "status", string(timeline.PostLatencyClassificationStatusExceeded))
	assertLogObjectIntField(t, classification, "threshold_millis", int(timeline.PostLatencyExceededThresholdMillis))
	assertLogObjectStringField(t, classification, "delay_source", string(timeline.PostDelaySourceNone))
	assertLogObjectStringField(t, classification, "internal_delay_cause", string(timeline.PostInternalDelayCauseJobFailure))
	assertLogObjectStringField(t, classification, "reason_code", string(timeline.PostLatencyReasonCodeJobFailure))
}

func newLoggedSQLiteDispatcherForFinalResultTest(t *testing.T, db *deliveryTestDB, sender *finalResultTestSender, config *dispatchstate.Config) (*Dispatcher, *bytes.Buffer) {
	t.Helper()

	logBuffer := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cache := cachemocks.NewLenientClient()

	return NewDispatcher(db, cache, sender, nil, logger, config), logBuffer
}

func findOutboxFinalResultAuditLogEntry(t *testing.T, logBuffer *bytes.Buffer) map[string]any {
	t.Helper()

	entries := findAllLogEntriesByMessage(t, logBuffer, deliveryAuditLogMessage)
	for i := range entries {
		raw, ok := entries[i][logschema.FieldTelemetrySource]
		if !ok {
			continue
		}

		value, ok := raw.(string)
		if !ok {
			t.Fatalf("telemetry_source type = %T, want string", raw)
		}

		if value == logschema.TelemetrySourceOutboxFinalResult {
			return entries[i]
		}
	}

	t.Fatalf("audit log with telemetry_source=%q not found in %s", logschema.TelemetrySourceOutboxFinalResult, logBuffer.String())

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

func readLatencyClassificationField(t *testing.T, entry map[string]any) map[string]any {
	t.Helper()

	field := logschema.FieldLatencyClassification
	raw, ok := entry[field]

	if !ok {
		t.Fatalf("log entry missing %q: %#v", field, entry)
	}

	value, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("log field %q type = %T, want object", field, raw)
	}

	return value
}

func readLogObjectStringField(t *testing.T, entry map[string]any, field string) string {
	t.Helper()

	raw, ok := entry[field]
	if !ok {
		t.Fatalf("log object missing %q: %#v", field, entry)
	}

	value, ok := raw.(string)
	if !ok {
		t.Fatalf("log object field %q type = %T, want string", field, raw)
	}

	return value
}

func readLogObjectIntField(t *testing.T, entry map[string]any, field string) int {
	t.Helper()

	raw, ok := entry[field]
	if !ok {
		t.Fatalf("log object missing %q: %#v", field, entry)
	}

	switch value := raw.(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		t.Fatalf("log object field %q type = %T, want number", field, raw)
	}

	return 0
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

func assertLogObjectStringField(t *testing.T, entry map[string]any, field, want string) {
	t.Helper()

	if got := readLogObjectStringField(t, entry, field); got != want {
		t.Fatalf("log object field %q = %q, want %q", field, got, want)
	}
}

func assertLogObjectIntField(t *testing.T, entry map[string]any, field string, want int) {
	t.Helper()

	if got := readLogObjectIntField(t, entry, field); got != want {
		t.Fatalf("log object field %q = %d, want %d", field, got, want)
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
