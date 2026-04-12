package runtime

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

func validatePublishedAtResolverSchema(ctx context.Context, postgresService database.Client) error {
	if postgresService == nil {
		return fmt.Errorf("postgres service is nil")
	}
	db := postgresService.GetGormDB()
	if db == nil || db.Migrator() == nil {
		return fmt.Errorf("gorm db or migrator is nil")
	}
	if !db.WithContext(ctx).Migrator().HasColumn(&domain.YouTubeCommunityShortsAlarmState{}, "published_at_retry_after") {
		return fmt.Errorf("missing migration 057: youtube_community_shorts_alarm_states.published_at_retry_after")
	}
	return nil
}
