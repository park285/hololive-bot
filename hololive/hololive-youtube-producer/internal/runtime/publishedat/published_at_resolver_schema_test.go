package publishedat

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestValidatePublishedAtResolverSchema_PassesWhenColumnExists(t *testing.T) {
	db := newPublishedAtResolverSchemaTestDB(t)
	require.NoError(t, db.AutoMigrate(&domain.YouTubeCommunityShortsAlarmState{}))
	createPublishedAtResolverIndexes(t, db)

	err := validatePublishedAtResolverSchema(context.Background(), &databasemocks.Client{
		GetGormDBFunc: func() *gorm.DB { return db },
	})
	require.NoError(t, err)
}

func TestValidatePublishedAtResolverSchema_FailsWhenColumnMissing(t *testing.T) {
	db := newPublishedAtResolverSchemaTestDB(t)
	require.NoError(t, db.Exec(`
		CREATE TABLE youtube_community_shorts_alarm_states (
			kind TEXT NOT NULL,
			post_id TEXT NOT NULL,
			content_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			actual_published_at DATETIME,
			detected_at DATETIME NOT NULL,
			authorized_at DATETIME,
			alarm_sent_at DATETIME,
			delivery_status TEXT NOT NULL DEFAULT 'DETECTED',
			created_at DATETIME NOT NULL,
			updated_at DATETIME NOT NULL,
			PRIMARY KEY (kind, post_id),
			UNIQUE(kind, content_id)
		)
	`).Error)

	err := validatePublishedAtResolverSchema(context.Background(), &databasemocks.Client{
		GetGormDBFunc: func() *gorm.DB { return db },
	})
	require.ErrorContains(t, err, "missing migration 057")
	require.ErrorContains(t, err, "published_at_retry_after")
}

func TestValidatePublishedAtResolverSchema_FailsWhenPendingResolutionIndexMissing(t *testing.T) {
	db := newPublishedAtResolverSchemaTestDB(t)
	require.NoError(t, db.AutoMigrate(&domain.YouTubeCommunityShortsAlarmState{}))
	require.NoError(t, db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_ycsas_pending_published_at_retry_after
		ON youtube_community_shorts_alarm_states (published_at_retry_after ASC, detected_at ASC, post_id ASC)
		WHERE actual_published_at IS NULL
		  AND alarm_sent_at IS NULL
		  AND authorized_at IS NULL
		  AND kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	`).Error)

	err := validatePublishedAtResolverSchema(context.Background(), &databasemocks.Client{
		GetGormDBFunc: func() *gorm.DB { return db },
	})
	require.ErrorContains(t, err, "missing migration 056 index")
}

func TestValidatePublishedAtResolverSchema_FailsWhenRetryAfterIndexMissing(t *testing.T) {
	db := newPublishedAtResolverSchemaTestDB(t)
	require.NoError(t, db.AutoMigrate(&domain.YouTubeCommunityShortsAlarmState{}))
	require.NoError(t, db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_ycsas_pending_published_at_resolution
		ON youtube_community_shorts_alarm_states (detected_at ASC, post_id ASC)
		WHERE actual_published_at IS NULL
		  AND alarm_sent_at IS NULL
		  AND authorized_at IS NULL
		  AND kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	`).Error)

	err := validatePublishedAtResolverSchema(context.Background(), &databasemocks.Client{
		GetGormDBFunc: func() *gorm.DB { return db },
	})
	require.ErrorContains(t, err, "missing migration 057 index")
}

func TestValidatePublishedAtResolverSchemaIfEnabled_SkipsWhenResolverDisabled(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	err := validatePublishedAtResolverSchemaIfEnabled(
		context.Background(),
		config.ScraperConfig{
			PublishedAtResolver: config.ScraperPublishedAtResolverConfig{
				Enabled:  false,
				Interval: 15 * time.Second,
			},
		},
		&databasemocks.Client{},
		logger,
	)
	require.NoError(t, err)
	assert.NotContains(t, logBuf.String(), `"msg":"published_at_resolver_schema_validated"`)
}

func TestValidatePublishedAtResolverSchemaIfEnabled_LogsWhenResolverEnabled(t *testing.T) {
	db := newPublishedAtResolverSchemaTestDB(t)
	require.NoError(t, db.AutoMigrate(&domain.YouTubeCommunityShortsAlarmState{}))
	createPublishedAtResolverIndexes(t, db)

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))

	err := validatePublishedAtResolverSchemaIfEnabled(
		context.Background(),
		config.ScraperConfig{
			PublishedAtResolver: config.ScraperPublishedAtResolverConfig{
				Enabled:  true,
				Interval: 15 * time.Second,
			},
		},
		&databasemocks.Client{
			GetGormDBFunc: func() *gorm.DB { return db },
		},
		logger,
	)
	require.NoError(t, err)
	assert.Contains(t, logBuf.String(), `"msg":"published_at_resolver_schema_validated"`)
}

func newPublishedAtResolverSchemaTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	return db
}

func createPublishedAtResolverIndexes(t *testing.T, db *gorm.DB) {
	t.Helper()

	require.NoError(t, db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_ycsas_pending_published_at_resolution
		ON youtube_community_shorts_alarm_states (detected_at ASC, post_id ASC)
		WHERE actual_published_at IS NULL
		  AND alarm_sent_at IS NULL
		  AND authorized_at IS NULL
		  AND kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	`).Error)
	require.NoError(t, db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_ycsas_pending_published_at_retry_after
		ON youtube_community_shorts_alarm_states (published_at_retry_after ASC, detected_at ASC, post_id ASC)
		WHERE actual_published_at IS NULL
		  AND alarm_sent_at IS NULL
		  AND authorized_at IS NULL
		  AND kind IN ('COMMUNITY_POST', 'NEW_SHORT')
	`).Error)
}
