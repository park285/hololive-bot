package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
)

var errFinalResultSendFailed = errors.New("send failed")

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
	ID            int64     `gorm:"primaryKey;autoIncrement"`
	Kind          string    `gorm:"type:text;not null"`
	ChannelID     string    `gorm:"type:text;not null"`
	ContentID     string    `gorm:"type:text;not null"`
	Payload       string    `gorm:"type:text;not null"`
	Status        string    `gorm:"type:text;not null"`
	AttemptCount  int       `gorm:"not null"`
	NextAttemptAt time.Time `gorm:"not null"`
	CreatedAt     time.Time
	LockedAt      *time.Time
	SentAt        *time.Time
	Error         string `gorm:"type:text"`
}

func (finalResultOutboxModel) TableName() string {
	return "youtube_notification_outbox"
}

type finalResultDeliveryModel struct {
	ID            int64     `gorm:"primaryKey;autoIncrement"`
	OutboxID      int64     `gorm:"not null;index:idx_ynd_outbox_room,unique"`
	RoomID        string    `gorm:"type:text;not null;index:idx_ynd_outbox_room,unique"`
	Status        string    `gorm:"type:text;not null"`
	AttemptCount  int       `gorm:"not null"`
	NextAttemptAt time.Time `gorm:"not null"`
	CreatedAt     time.Time
	LockedAt      *time.Time
	SentAt        *time.Time
	Error         string `gorm:"type:text"`
}

func (finalResultDeliveryModel) TableName() string {
	return "youtube_notification_delivery"
}

type finalResultTrackingModel struct {
	Kind                        string `gorm:"primaryKey;type:text"`
	ContentID                   string `gorm:"primaryKey;type:text"`
	CanonicalContentID          string
	ChannelID                   string `gorm:"type:text;not null"`
	ActualPublishedAt           *time.Time
	DetectedAt                  time.Time `gorm:"not null"`
	AlarmSentAt                 *time.Time
	AlarmLatencyMillis          *int64
	AlarmLatencyExceeded        *bool
	DeliveryStatus              string `gorm:"type:text;not null;default:'PENDING'"`
	LatencyClassificationStatus string
	DelaySource                 string
	InternalDelayCause          string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

func (finalResultTrackingModel) TableName() string {
	return "youtube_content_alarm_tracking"
}

type finalResultTelemetryBufferModel struct {
	ID                          int64      `gorm:"primaryKey;autoIncrement"`
	DeliveryID                  int64      `gorm:"not null;uniqueIndex:idx_ydt_delivery_attempt"`
	AttemptOrdinal              int        `gorm:"not null;uniqueIndex:idx_ydt_delivery_attempt"`
	OutboxID                    int64      `gorm:"not null"`
	ChannelID                   string     `gorm:"type:text;not null"`
	ContentID                   string     `gorm:"type:text;not null"`
	PostID                      string     `gorm:"type:text;not null"`
	RoomID                      string     `gorm:"type:text;not null"`
	AlarmType                   string     `gorm:"type:text;not null"`
	ActualPublishedAt           *time.Time `gorm:"column:actual_published_at"`
	AlarmSentAt                 *time.Time `gorm:"column:alarm_sent_at"`
	AlarmLatencyMillis          *int64     `gorm:"column:alarm_latency_millis"`
	DetectedAt                  *time.Time `gorm:"column:detected_at"`
	ObservationStatus           string     `gorm:"column:observation_status;type:text;not null"`
	ObservationRuntimeName      string     `gorm:"column:observation_runtime_name;type:text"`
	ObservationBigBangCutoverAt *time.Time `gorm:"column:observation_bigbang_cutover_at"`
	ObservationStartedAt        *time.Time `gorm:"column:observation_started_at"`
	ObservationEndedAt          *time.Time `gorm:"column:observation_ended_at"`
	DedupeKey                   string     `gorm:"type:text;not null"`
	DeliveryPath                string     `gorm:"type:text;not null"`
	DeliveryMode                string     `gorm:"type:text;not null"`
	SendResult                  string     `gorm:"type:text;not null"`
	FailureReason               string     `gorm:"type:text"`
	AttemptStartedAt            *time.Time
	AttemptFinishedAt           *time.Time
	EventAt                     time.Time `gorm:"not null"`
	NextAttemptAt               time.Time `gorm:"not null"`
	CreatedAt                   time.Time
	LockedAt                    *time.Time
	LoggedAt                    *time.Time
	Error                       string `gorm:"type:text"`
}

func (finalResultTelemetryBufferModel) TableName() string {
	return "youtube_notification_delivery_telemetry"
}

func TestProcessPendingDeliveries_LogsCommunityShortsFinalSuccessResult(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&finalResultOutboxModel{},
		&finalResultDeliveryModel{},
		&finalResultTrackingModel{},
		&finalResultTelemetryBufferModel{},
		&domain.YouTubeCommunityShortsObservationWindow{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	now := time.Now().UTC()
	actualPublishedAt := now.Add(-190 * time.Second)
	detectedAt := now.Add(-150 * time.Second)
	item := finalResultOutboxModel{
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_final_success",
		ContentID:     "short-final-success",
		Payload:       `{"video_id":"short-final-success","title":"short title"}`,
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.Create(&finalResultTrackingModel{
		Kind:              string(domain.OutboxKindNewShort),
		ContentID:         item.ContentID,
		ChannelID:         item.ChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}).Error)
	require.NoError(t, db.Create(&finalResultDeliveryModel{
		OutboxID:      item.ID,
		RoomID:        "room-success",
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}).Error)

	dispatcher, logBuffer := newLoggedSQLiteDispatcherForFinalResultTest(t, db, &finalResultTestSender{failRoom: map[string]bool{}}, Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.processPendingDeliveries(ctx)

	entry := findAuditLogEntryByTelemetrySource(t, logBuffer, logschema.TelemetrySourceOutboxFinalResult)
	assertLogStringField(t, entry, deliveryAuditContentIDLogField, "short-final-success")
	assertLogStringField(t, entry, deliveryAuditPostIDLogField, "short-final-success")
	assertLogStringField(t, entry, deliveryAuditAlarmTypeLogField, string(domain.AlarmTypeShorts))
	assertLogStringField(t, entry, deliveryAuditSendResultLogField, "success")
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
	classification := readLogObjectField(t, entry, logschema.FieldLatencyClassification)
	assertLogObjectStringField(t, classification, "status", string(PostLatencyClassificationStatusExceeded))
	assertLogObjectIntField(t, classification, "threshold_millis", int(postLatencyExceededThresholdMillis))
	assertLogObjectStringField(t, classification, "delay_source", string(PostDelaySourceInternalDelivery))
	assertLogObjectStringField(t, classification, "internal_delay_cause", string(PostInternalDelayCauseQueueWait))
	assertLogObjectStringField(t, classification, "reason_code", string(PostLatencyReasonCodeQueueWait))
}

func TestProcessPendingDeliveries_LogsCommunityShortsFinalCommunitySuccessResult(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&finalResultOutboxModel{},
		&finalResultDeliveryModel{},
		&finalResultTrackingModel{},
		&finalResultTelemetryBufferModel{},
		&domain.YouTubeCommunityShortsObservationWindow{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	now := time.Now().UTC()
	actualPublishedAt := now.Add(-190 * time.Second)
	detectedAt := now.Add(-150 * time.Second)
	item := finalResultOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_final_community_success",
		ContentID:     "post-final-community-success",
		Payload:       `{"canonical_post_id":"post-final-community-success","post_id":"post-resource","content_text":"community title"}`,
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.Create(&finalResultTrackingModel{
		Kind:              string(domain.OutboxKindCommunityPost),
		ContentID:         item.ContentID,
		ChannelID:         item.ChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}).Error)
	require.NoError(t, db.Create(&finalResultDeliveryModel{
		OutboxID:      item.ID,
		RoomID:        "room-community-success",
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}).Error)

	dispatcher, logBuffer := newLoggedSQLiteDispatcherForFinalResultTest(t, db, &finalResultTestSender{failRoom: map[string]bool{}}, Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.processPendingDeliveries(ctx)

	entry := findAuditLogEntryByTelemetrySource(t, logBuffer, logschema.TelemetrySourceOutboxFinalResult)
	assertLogStringField(t, entry, deliveryAuditContentIDLogField, "post-final-community-success")
	assertLogStringField(t, entry, deliveryAuditPostIDLogField, "post-final-community-success")
	assertLogStringField(t, entry, deliveryAuditAlarmTypeLogField, string(domain.AlarmTypeCommunity))
	assertLogStringField(t, entry, deliveryAuditSendResultLogField, "success")
	assertLogStringField(t, entry, deliveryAuditModeLogField, logschema.DeliveryModeFinalResult)
	assertLogTimeField(t, entry, logschema.FieldActualPublishedAt, actualPublishedAt)
	assertLogStringField(t, entry, deliveryDedupeKeyLogField, "youtube-notification:COMMUNITY_POST:post-final-community-success")
	assertLogTimeField(t, entry, logschema.FieldAlarmSentAt)
	require.GreaterOrEqual(t, readLogIntField(t, entry, logschema.FieldAlarmLatencyMillis), 190000)
	assertLogIntField(t, entry, logschema.FieldTargetRoomCount, 1)
	assertLogIntField(t, entry, logschema.FieldSuccessfulRoomCount, 1)
	assertLogIntField(t, entry, logschema.FieldFailedRoomCount, 0)
	assertLogTimeField(t, entry, deliveryAuditSentAtLogField)
	classification := readLogObjectField(t, entry, logschema.FieldLatencyClassification)
	assertLogObjectStringField(t, classification, "status", string(PostLatencyClassificationStatusExceeded))
	assertLogObjectIntField(t, classification, "threshold_millis", int(postLatencyExceededThresholdMillis))
	assertLogObjectStringField(t, classification, "delay_source", string(PostDelaySourceInternalDelivery))
	assertLogObjectStringField(t, classification, "internal_delay_cause", string(PostInternalDelayCauseQueueWait))
	assertLogObjectStringField(t, classification, "reason_code", string(PostLatencyReasonCodeQueueWait))
}

func TestProcessPendingDeliveries_LogsCommunityShortsFinalExternalDelayReasonCode(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&finalResultOutboxModel{},
		&finalResultDeliveryModel{},
		&finalResultTrackingModel{},
		&finalResultTelemetryBufferModel{},
		&domain.YouTubeCommunityShortsObservationWindow{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	now := time.Now().UTC()
	actualPublishedAt := now.Add(-210 * time.Second)
	detectedAt := now.Add(-15 * time.Second)
	item := finalResultOutboxModel{
		Kind:          string(domain.OutboxKindNewShort),
		ChannelID:     "UC_final_external_delay",
		ContentID:     "short-final-external-delay",
		Payload:       `{"video_id":"short-final-external-delay","title":"short title"}`,
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.Create(&finalResultTrackingModel{
		Kind:              string(domain.OutboxKindNewShort),
		ContentID:         item.ContentID,
		ChannelID:         item.ChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}).Error)
	require.NoError(t, db.Create(&finalResultDeliveryModel{
		OutboxID:      item.ID,
		RoomID:        "room-external-delay",
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}).Error)

	dispatcher, logBuffer := newLoggedSQLiteDispatcherForFinalResultTest(t, db, &finalResultTestSender{failRoom: map[string]bool{}}, Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.processPendingDeliveries(ctx)

	entry := findAuditLogEntryByTelemetrySource(t, logBuffer, logschema.TelemetrySourceOutboxFinalResult)
	classification := readLogObjectField(t, entry, logschema.FieldLatencyClassification)
	assertLogObjectStringField(t, classification, "status", string(PostLatencyClassificationStatusExceeded))
	assertLogObjectStringField(t, classification, "delay_source", string(PostDelaySourceExternalCollection))
	assertLogObjectStringField(t, classification, "internal_delay_cause", string(PostInternalDelayCauseQueueWait))
	assertLogObjectStringField(t, classification, "reason_code", string(PostLatencyReasonCodeExternalCollection))
}

func TestProcessPendingDeliveries_LogsCommunityShortsFinalFailureReason(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&finalResultOutboxModel{},
		&finalResultDeliveryModel{},
		&finalResultTrackingModel{},
		&finalResultTelemetryBufferModel{},
		&domain.YouTubeCommunityShortsObservationWindow{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	now := time.Now().UTC()
	actualPublishedAt := now.Add(-200 * time.Second)
	detectedAt := now.Add(-160 * time.Second)
	item := finalResultOutboxModel{
		Kind:          string(domain.OutboxKindCommunityPost),
		ChannelID:     "UC_final_failure",
		ContentID:     "post-final-failure",
		Payload:       `{"canonical_post_id":"post-final-failure","post_id":"post-resource","content_text":"community title"}`,
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}
	require.NoError(t, db.Create(&item).Error)
	require.NoError(t, db.Create(&finalResultTrackingModel{
		Kind:              string(domain.OutboxKindCommunityPost),
		ContentID:         item.ContentID,
		ChannelID:         item.ChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}).Error)
	require.NoError(t, db.Create(&finalResultDeliveryModel{
		OutboxID:      item.ID,
		RoomID:        "room-failure",
		Status:        string(domain.OutboxStatusPending),
		NextAttemptAt: now,
	}).Error)

	dispatcher, logBuffer := newLoggedSQLiteDispatcherForFinalResultTest(t, db, &finalResultTestSender{failRoom: map[string]bool{"room-failure": true}}, Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          1,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	dispatcher.processPendingDeliveries(ctx)

	entry := findAuditLogEntryByTelemetrySource(t, logBuffer, logschema.TelemetrySourceOutboxFinalResult)
	assertLogStringField(t, entry, deliveryAuditContentIDLogField, "post-final-failure")
	assertLogStringField(t, entry, deliveryAuditPostIDLogField, "post-final-failure")
	assertLogStringField(t, entry, deliveryAuditAlarmTypeLogField, string(domain.AlarmTypeCommunity))
	assertLogStringField(t, entry, deliveryAuditSendResultLogField, "failure")
	assertLogStringField(t, entry, deliveryAuditFailureReasonLogField, "send message")
	assertLogStringField(t, entry, deliveryAuditModeLogField, logschema.DeliveryModeFinalResult)
	assertLogTimeField(t, entry, logschema.FieldActualPublishedAt, actualPublishedAt)
	assertLogIntField(t, entry, logschema.FieldTargetRoomCount, 1)
	assertLogIntField(t, entry, logschema.FieldSuccessfulRoomCount, 0)
	assertLogIntField(t, entry, logschema.FieldFailedRoomCount, 1)
	assertLogTimeField(t, entry, deliveryAuditSentAtLogField)
	classification := readLogObjectField(t, entry, logschema.FieldLatencyClassification)
	assertLogObjectStringField(t, classification, "status", string(PostLatencyClassificationStatusExceeded))
	assertLogObjectIntField(t, classification, "threshold_millis", int(postLatencyExceededThresholdMillis))
	assertLogObjectStringField(t, classification, "delay_source", string(PostDelaySourceNone))
	assertLogObjectStringField(t, classification, "internal_delay_cause", string(PostInternalDelayCauseJobFailure))
	assertLogObjectStringField(t, classification, "reason_code", string(PostLatencyReasonCodeJobFailure))
}

func newLoggedSQLiteDispatcherForFinalResultTest(t *testing.T, db *gorm.DB, sender *finalResultTestSender, cfg Config) (*Dispatcher, *bytes.Buffer) {
	t.Helper()

	logBuffer := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cache := cachemocks.NewLenientClient()
	return NewDispatcher(db, cache, sender, nil, logger, cfg), logBuffer
}

func findAuditLogEntryByTelemetrySource(t *testing.T, logBuffer *bytes.Buffer, source string) map[string]any {
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
		if value == source {
			return entries[i]
		}
	}

	t.Fatalf("audit log with telemetry_source=%q not found in %s", source, logBuffer.String())
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
		if err := json.Unmarshal(line, &entry); err != nil {
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

func readLogObjectField(t *testing.T, entry map[string]any, field string) map[string]any {
	t.Helper()

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
