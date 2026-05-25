package observation

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/kapu/hololive-shared/pkg/domain"
)

var trackingTestDBSequence uint64

func newTrackingTestDB(t *testing.T) *gorm.DB {
	return newTrackingTestDBWithMaxOpenConns(t, 1)
}

func newTrackingTestDBWithMaxOpenConns(t *testing.T, maxOpenConns int) *gorm.DB {
	t.Helper()
	if maxOpenConns < 1 {
		maxOpenConns = 1
	}

	dsn := fmt.Sprintf("file:%s_%d?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"), atomic.AddUint64(&trackingTestDBSequence, 1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(maxOpenConns)
	require.NoError(t, db.AutoMigrate(&domain.YouTubeContentAlarmTracking{}, &domain.YouTubeCommunityShortsSourcePost{}, &domain.YouTubeCommunityShortsAlarmState{}, &domain.YouTubeCommunityShortsObservationWindow{}, &domain.YouTubeCommunityShortsObservationPostBaseline{}))
	return db
}
