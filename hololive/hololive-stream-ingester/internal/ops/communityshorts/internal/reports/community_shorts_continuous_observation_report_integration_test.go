package reports

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	communityshorts "github.com/kapu/hololive-stream-ingester/internal/communityshorts"
)

func TestCollectCommunityShortsContinuousObservationReportWithSession_ActiveObservation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	cutoverAt := now.Add(-2 * time.Hour)
	session := newCommunityShortsContinuousObservationReportTestSession(t)
	seedCommunityShortsObservationWindow(t, session.db, communityShortsObservationWindowSeed{
		runtimeName:      "youtube-scraper",
		cutoverAt:        cutoverAt,
		deploymentAt:     cutoverAt,
		observationStart: cutoverAt,
		observationEnd:   now.Add(22 * time.Hour),
	})

	report, err := collectCommunityShortsContinuousObservationReportWithSession(
		context.Background(),
		session,
		&config.Config{},
		testCommunityShortsObservationLogger(),
		now,
		CommunityShortsContinuousObservationCollectOptions{
			ObservationRuntimeName:      "youtube-scraper",
			ObservationBigBangCutoverAt: cutoverAt,
		},
		dbBackedCommunityShortsContinuousObservationWiring(),
	)
	require.NoError(t, err)
	require.Equal(t, CommunityShortsContinuousObservationStatusActive, report.Observation.Status)
	require.Nil(t, report.AlarmSentHistoryDataset)
	require.Equal(t, CommunityShortsContinuousObservationCloseoutStatusPending, report.Closeout24H.Status)
}

func TestCollectCommunityShortsContinuousObservationReportWithSession_FinalizedObservation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	cutoverAt := now.Add(-26 * time.Hour)
	observationEnd := cutoverAt.Add(24 * time.Hour)
	finalizedAt := observationEnd
	session := newCommunityShortsContinuousObservationReportTestSession(t)
	seedCommunityShortsObservationWindow(t, session.db, communityShortsObservationWindowSeed{
		runtimeName:      "youtube-scraper",
		cutoverAt:        cutoverAt,
		deploymentAt:     cutoverAt,
		observationStart: cutoverAt,
		observationEnd:   observationEnd,
		closedAt:         &observationEnd,
		finalizedAt:      &finalizedAt,
	})

	report, err := collectCommunityShortsContinuousObservationReportWithSession(
		context.Background(),
		session,
		&config.Config{},
		testCommunityShortsObservationLogger(),
		now,
		CommunityShortsContinuousObservationCollectOptions{
			ObservationRuntimeName:      "youtube-scraper",
			ObservationBigBangCutoverAt: cutoverAt,
		},
		dbBackedCommunityShortsContinuousObservationWiring(),
	)
	require.NoError(t, err)
	require.Equal(t, CommunityShortsContinuousObservationStatusFinalized, report.Observation.Status)
	require.NotNil(t, report.AlarmSentHistoryDataset)
	require.Equal(t, 0, report.AlarmSentHistoryDataset.Summary.ReferenceRowCount)
}

func TestCollectCommunityShortsContinuousObservationReportWithSession_UsesInsufficientEvidenceWhenDatasetUnavailable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	cutoverAt := now.Add(-26 * time.Hour)
	observationEnd := cutoverAt.Add(24 * time.Hour)
	finalizedAt := observationEnd
	session := newCommunityShortsContinuousObservationReportTestSession(t)
	seedCommunityShortsObservationWindow(t, session.db, communityShortsObservationWindowSeed{
		runtimeName:      "youtube-scraper",
		cutoverAt:        cutoverAt,
		deploymentAt:     cutoverAt,
		observationStart: cutoverAt,
		observationEnd:   observationEnd,
		closedAt:         &observationEnd,
		finalizedAt:      &finalizedAt,
	})

	wiring := dbBackedCommunityShortsContinuousObservationWiring()
	wiring.collectAlarmSentHistoryDataset = func(context.Context, *communityShortsOpsSession, time.Time, CommunityShortsAlarmSentHistoryDatasetQuery) (CommunityShortsAlarmSentHistoryDatasetReport, error) {
		return CommunityShortsAlarmSentHistoryDatasetReport{}, fmt.Errorf("dataset unavailable")
	}

	report, err := collectCommunityShortsContinuousObservationReportWithSession(
		context.Background(),
		session,
		&config.Config{},
		testCommunityShortsObservationLogger(),
		now,
		CommunityShortsContinuousObservationCollectOptions{
			ObservationRuntimeName:      "youtube-scraper",
			ObservationBigBangCutoverAt: cutoverAt,
		},
		wiring,
	)
	require.NoError(t, err)
	require.Nil(t, report.AlarmSentHistoryDataset)
	require.Equal(t, CommunityShortsContinuousObservationCloseoutStatusInsufficientEvidence, report.MissingAlarmCloseout24H.Status)
	require.Equal(t, CommunityShortsContinuousObservationCloseoutStatusInsufficientEvidence, report.StateConsistencyCloseout24H.Status)
	require.Contains(t, strings.ToLower(report.MissingAlarmCloseout24H.Statement), "dataset unavailable")
	require.Contains(t, strings.ToLower(report.StateConsistencyCloseout24H.Statement), "dataset unavailable")
}

type communityShortsObservationWindowSeed struct {
	runtimeName      string
	cutoverAt        time.Time
	deploymentAt     time.Time
	observationStart time.Time
	observationEnd   time.Time
	closedAt         *time.Time
	finalizedAt      *time.Time
}

func testCommunityShortsObservationLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func dbBackedCommunityShortsContinuousObservationWiring() communityShortsContinuousObservationCollectorWiring {
	wiring := defaultCommunityShortsContinuousObservationCollectorWiring()
	wiring.collectTargetBaseline = func(context.Context, *communityShortsOpsSession, *config.Config, *slog.Logger, time.Time) (communityshorts.TargetBaseline, error) {
		return communityshorts.TargetBaseline{}, nil
	}
	return wiring
}

func newCommunityShortsContinuousObservationReportTestSession(t *testing.T) *communityShortsOpsSession {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&sqliteObservationCommunityPostModel{},
		&sqliteObservationVideoModel{},
		&sqliteObservationTrackingModel{},
		&sqliteObservationAlarmStateModel{},
		&domain.YouTubeCommunityShortsObservationWindow{},
		&domain.YouTubeCommunityShortsObservationPostBaseline{},
		&sqliteObservationOutboxModel{},
		&sqliteObservationDeliveryTelemetryModel{},
	))

	session := newCommunityShortsOpsSession(db)
	return session
}

func seedCommunityShortsObservationWindow(t *testing.T, db *gorm.DB, seed communityShortsObservationWindowSeed) {
	t.Helper()

	window := domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName:             seed.runtimeName,
		BigBangCutoverAt:        seed.cutoverAt,
		AppVersion:              "2.0.99",
		TargetChannelCount:      1,
		DeploymentCompletedAt:   seed.deploymentAt,
		ObservationStartedAt:    seed.observationStart,
		ObservationEndedAt:      seed.observationEnd,
		ClosedAt:                seed.closedAt,
		FinalizedPostBaselineAt: seed.finalizedAt,
	}
	require.NoError(t, db.Create(&window).Error)
}

type sqliteObservationCommunityPostModel struct {
	PostID      string `gorm:"primaryKey;size:50"`
	ContentText string `gorm:"type:text"`
	PublishedAt *time.Time
}

func (sqliteObservationCommunityPostModel) TableName() string {
	return "youtube_community_posts"
}

type sqliteObservationVideoModel struct {
	VideoID     string `gorm:"primaryKey;size:20"`
	Title       string `gorm:"size:500"`
	PublishedAt *time.Time
}

func (sqliteObservationVideoModel) TableName() string {
	return "youtube_videos"
}

type sqliteObservationTrackingModel struct {
	Kind                        domain.OutboxKind `gorm:"primaryKey;size:20"`
	ContentID                   string            `gorm:"primaryKey;size:50"`
	CanonicalContentID          string            `gorm:"size:50"`
	ChannelID                   string            `gorm:"size:50;not null"`
	ActualPublishedAt           *time.Time
	DetectedAt                  time.Time `gorm:"not null"`
	AlarmSentAt                 *time.Time
	AlarmLatencyMillis          *int64
	AlarmLatencyExceeded        *bool
	DeliveryStatus              string `gorm:"size:20;not null;default:'PENDING'"`
	LatencyClassificationStatus string
	DelaySource                 string
	InternalDelayCause          string
	CreatedAt                   time.Time
	UpdatedAt                   time.Time
}

func (sqliteObservationTrackingModel) TableName() string {
	return "youtube_content_alarm_tracking"
}

type sqliteObservationAlarmStateModel struct {
	Kind                  domain.OutboxKind `gorm:"primaryKey;size:20"`
	PostID                string            `gorm:"primaryKey;size:50"`
	ContentID             string            `gorm:"size:50;not null"`
	ChannelID             string            `gorm:"size:50;not null"`
	ActualPublishedAt     *time.Time
	DetectedAt            time.Time `gorm:"not null"`
	PublishedAtRetryAfter *time.Time
	AuthorizedAt          *time.Time
	AlarmSentAt           *time.Time
	DeliveryStatus        string `gorm:"size:20;not null;default:'DETECTED'"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

func (sqliteObservationAlarmStateModel) TableName() string {
	return "youtube_community_shorts_alarm_states"
}

type sqliteObservationOutboxModel struct {
	ID            int64 `gorm:"primaryKey;autoIncrement"`
	Kind          domain.OutboxKind
	ChannelID     string
	ContentID     string
	Payload       string
	Status        string
	AttemptCount  int
	NextAttemptAt time.Time
	CreatedAt     time.Time
	LockedAt      *time.Time
	SentAt        *time.Time
	Error         string
}

func (sqliteObservationOutboxModel) TableName() string {
	return "youtube_notification_outbox"
}

type sqliteObservationDeliveryTelemetryModel struct {
	ID                          int64 `gorm:"primaryKey;autoIncrement"`
	DeliveryID                  int64
	AttemptOrdinal              int
	OutboxID                    int64
	ChannelID                   string
	ContentID                   string
	PostID                      string
	RoomID                      string
	AlarmType                   domain.AlarmType
	ActualPublishedAt           *time.Time
	AlarmSentAt                 *time.Time
	AlarmLatencyMillis          *int64
	DetectedAt                  *time.Time
	ObservationStatus           string
	ObservationRuntimeName      string
	ObservationBigBangCutoverAt *time.Time `gorm:"column:observation_bigbang_cutover_at"`
	ObservationStartedAt        *time.Time
	ObservationEndedAt          *time.Time
	DedupeKey                   string
	DeliveryPath                string
	DeliveryMode                string
	SendResult                  string
	FailureReason               string
	AttemptStartedAt            *time.Time
	AttemptFinishedAt           *time.Time
	EventAt                     time.Time
	NextAttemptAt               time.Time
	CreatedAt                   time.Time
	LockedAt                    *time.Time
	LoggedAt                    *time.Time
	Error                       string
}

func (sqliteObservationDeliveryTelemetryModel) TableName() string {
	return "youtube_notification_delivery_telemetry"
}
