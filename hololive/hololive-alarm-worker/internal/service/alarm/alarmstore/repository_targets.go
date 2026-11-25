package alarmstore

import (
	"context"
	"fmt"
	"time"

	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const channelSubscriberLoadTimeout = 5 * time.Second

func (r *Repository) loadChannelSubscriberAlarms(
	ctx context.Context,
	channelID string,
	alarmType domain.AlarmType,
) ([]*domain.Alarm, error) {
	queryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), channelSubscriberLoadTimeout)
	defer cancel()

	out, err := r.FindByChannelAndType(queryCtx, channelID, alarmType)
	if err != nil {
		return out, fmt.Errorf("load channel subscriber alarms: %w", err)
	}

	return out, nil
}

func (r *Repository) loadChannelSubscriberAlarmsByChannels(
	ctx context.Context,
	channelIDs []string,
	alarmType domain.AlarmType,
) (map[string][]*domain.Alarm, error) {
	queryCtx, cancel := context.WithTimeout(ctx, channelSubscriberLoadTimeout)
	defer cancel()

	rows, err := pgxdb.Query(queryCtx, r.pool, loadChannelSubscriberAlarmsByChannelsSQL, channelIDs, string(alarmType))
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
