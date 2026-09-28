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

// loadChannelSubscriberAlarmsByChannels는 여러 채널의 알림 종류별 구독 레코드를 한 번의 조회로 읽어 채널별로 묶는다.
func (r *Repository) loadChannelSubscriberAlarmsByChannels(
	ctx context.Context,
	channelIDs []string,
	alarmType domain.AlarmType,
) (map[string][]*domain.Alarm, error) {
	queryCtx, cancel := context.WithTimeout(ctx, channelSubscriberLoadTimeout)
	defer cancel()

	rows, err := r.pool.Query(queryCtx, mustSQL("repository_targets_0047_01.sql"), channelIDs, string(alarmType))
	if err != nil {
		return nil, fmt.Errorf("load channel subscriber alarms by channels: %w", err)
	}
	defer rows.Close()

	alarms, err := r.scanAlarms(rows)
	if err != nil {
		return nil, fmt.Errorf("load channel subscriber alarms by channels: %w", err)
	}

	out := make(map[string][]*domain.Alarm, len(channelIDs))

	for _, alarmRecord := range alarms {
		out[alarmRecord.ChannelID] = append(out[alarmRecord.ChannelID], alarmRecord)
	}

	return out, nil
}
