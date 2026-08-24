package observation

import (
	"context"
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *alarmStateRepository) FindAlarmStateByPostID(ctx context.Context, kind domain.OutboxKind, postID string) (*domain.YouTubeCommunityShortsAlarmState, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("find alarm state by post id: db is nil")
	}

	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(kind, postID)
	if err != nil {
		return nil, fmt.Errorf("find alarm state by post id: %w", err)
	}

	var row domain.YouTubeCommunityShortsAlarmState

	found, err := dbx.GetSQL(ctx, r.db, &row, "find alarm state by post id: query row", mustSQL("alarm_state_repository_0022_01.sql"), normalizedKind, normalizedPostID)
	if err != nil {
		return nil, fmt.Errorf("get SQL: %w", err)
	}

	if !found {
		//nolint:nilnil // 미존재는 오류가 아니며, alarm-worker의 claim_manager가 state == nil 자체로 분기한다. sentinel 오류로 바꾸려면 이 패키지 밖 호출자 3곳을 함께 고쳐야 한다.
		return nil, nil
	}

	return &row, nil
}
