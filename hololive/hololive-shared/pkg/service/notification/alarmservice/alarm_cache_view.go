package alarmservice

import (
	"context"
	"fmt"
	"time"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
)

// ListRoomAlarmsView는 채널·멤버별 구독을 독립적으로 표시하고 다른 멤버의 다음 방송은 제외한다.
func (as *AlarmService) ListRoomAlarmsView(ctx context.Context, roomID string) ([]domain.AlarmListView, error) {
	startedAt := time.Now()

	var opErr error

	defer func() {
		observeAlarmServiceOperation("list_view", startedAt, opErr)
	}()

	alarms, err := as.GetRoomAlarmsWithTypes(ctx, roomID)
	if err != nil {
		opErr = fmt.Errorf("list room alarms view: %w", err)
		return nil, opErr
	}

	if len(alarms) == 0 {
		return []domain.AlarmListView{}, nil
	}

	channelIDs := make([]string, 0, len(alarms))
	for _, alarm := range alarms {
		channelIDs = append(channelIDs, alarm.ChannelID)
	}

	memberNames, err := as.getMemberNamesBatch(ctx, channelIDs)
	if err != nil {
		opErr = fmt.Errorf("list room alarms view: get member names batch: %w", err)
		return nil, opErr
	}

	nextStreams, err := as.getNextStreamInfosBatch(ctx, channelIDs)
	if err != nil {
		opErr = fmt.Errorf("list room alarms view: get next stream info batch: %w", err)
		return nil, opErr
	}

	return buildAlarmListViews(alarms, memberNames, nextStreams), nil
}

func buildAlarmListViews(
	alarms []*domain.Alarm,
	memberNames map[string]string,
	nextStreams map[string]*domain.NextStreamInfo,
) []domain.AlarmListView {
	entries := make([]domain.AlarmListView, 0, len(alarms))
	for _, alarm := range alarms {
		memberName := stringutil.TrimSpace(memberNames[alarm.ChannelID])
		if memberName == "" {
			memberName = stringutil.TrimSpace(alarm.MemberName)
		}

		if memberName == "" {
			memberName = alarm.ChannelID
		}

		if host, ok := mekparkhost.SubscriptionMember(alarm.ChannelID, alarm.HostID); ok {
			memberName = host.Name
		}

		nextStream := nextStreams[alarm.ChannelID]
		if alarm.HostID != "" && (!alarm.AlarmTypes.Contains(domain.AlarmTypeLive) ||
			nextStream != nil && !mekparkhost.Identify(alarm.ChannelID, nextStream.Title).MatchesSubscription(alarm.HostID)) {
			nextStream = nil
		}

		entries = append(entries, domain.AlarmListView{
			ChannelID:  alarm.ChannelID,
			HostID:     alarm.HostID,
			MemberName: memberName,
			AlarmTypes: alarm.AlarmTypes,
			NextStream: nextStream,
		})
	}

	return entries
}

func (as *AlarmService) getMemberNamesBatch(ctx context.Context, channelIDs []string) (map[string]string, error) {
	out, err := as.cacheState.GetMemberNamesBatch(ctx, channelIDs)
	if err != nil {
		return nil, fmt.Errorf("get member names batch: %w", err)
	}

	return out, nil
}
