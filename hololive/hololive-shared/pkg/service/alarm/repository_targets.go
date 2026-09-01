package alarm

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const channelSubscriberLoadTimeout = 5 * time.Second

func (r *Repository) loadChannelSubscriberAlarms(
	ctx context.Context,
	channelID string,
	alarmType domain.AlarmType,
) ([]*domain.Alarm, error) {
	// A shared singleflight query must not inherit the first caller's deadline:
	// a short deadline would fail all followers, while a long deadline would
	// bypass this bounded repository operation.
	queryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), channelSubscriberLoadTimeout)
	defer cancel()

	rows, err := r.pool.Query(queryCtx, mustSQL("targets_0188_01.sql"), channelID, string(alarmType))
	if err != nil {
		return nil, fmt.Errorf("load channel subscriber alarms: %w", err)
	}
	defer rows.Close()

	out, err := r.scanAlarms(rows)
	if err != nil {
		return out, fmt.Errorf("load channel subscriber alarms: %w", err)
	}

	return out, nil
}
