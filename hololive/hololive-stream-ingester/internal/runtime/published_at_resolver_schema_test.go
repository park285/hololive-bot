package runtime

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/kapu/hololive-shared/pkg/domain"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
)

func TestValidatePublishedAtResolverSchema_PassesWhenColumnExists(t *testing.T) {
	db := newPublishedAtResolverSchemaTestDB(t)
	require.NoError(t, db.AutoMigrate(&domain.YouTubeCommunityShortsAlarmState{}))

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

func newPublishedAtResolverSchemaTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	return db
}
