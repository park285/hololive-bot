package observation

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *alarmStateRepository) FindAlarmStateByPostID(ctx context.Context, kind domain.OutboxKind, postID string) (*domain.YouTubeCommunityShortsAlarmState, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("find alarm state by post id: db is nil")
	}

	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(kind, postID)
	if err != nil {
		return nil, fmt.Errorf("find alarm state by post id: %w", err)
	}

	var row domain.YouTubeCommunityShortsAlarmState
	found, err := dbx.GetSQL(ctx, r.db, &row, "find alarm state by post id: query row", mustSQL("alarm_state_repository_0022_01.sql"), normalizedKind, normalizedPostID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	return &row, nil
}
