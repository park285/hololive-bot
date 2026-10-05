package subscriptions

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
)

// ListRoomAlarmsView는 채널·멤버별 구독을 독립적으로 표시한다.
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

	memberNames, err = as.resolveMissingMemberNames(ctx, alarms, memberNames)
	if err != nil {
		opErr = fmt.Errorf("list room alarms view: %w", err)
		return nil, opErr
	}

	return buildAlarmListViews(alarms, memberNames), nil
}

// resolveMissingMemberNames는 이름 캐시에 없는 채널을 members 정본(short_korean_name→korean_name→english_name)으로 채운다.
// 조회 오류는 다른 이름으로 덮지 않고 돌려준다. members에도 이름이 없는 채널만 목록에 채널 ID로 남는다.
func (as *AlarmService) resolveMissingMemberNames(ctx context.Context, alarms []*domain.Alarm, cached map[string]string) (map[string]string, error) {
	resolved := maps.Clone(cached)
	if resolved == nil {
		resolved = make(map[string]string, len(alarms))
	}

	for _, alarm := range alarms {
		if stringutil.TrimSpace(resolved[alarm.ChannelID]) != "" {
			continue
		}

		name, err := as.resolveCacheMemberName(ctx, alarm.ChannelID)
		if err != nil {
			return nil, fmt.Errorf("resolve member name: %w", err)
		}

		resolved[alarm.ChannelID] = name
	}

	return resolved, nil
}

func buildAlarmListViews(alarms []*domain.Alarm, memberNames map[string]string) []domain.AlarmListView {
	entries := make([]domain.AlarmListView, 0, len(alarms))
	for _, alarm := range alarms {
		memberName := stringutil.TrimSpace(memberNames[alarm.ChannelID])
		if memberName == "" {
			memberName = alarm.ChannelID
		}

		if host, ok := mekparkhost.SubscriptionMember(alarm.ChannelID, alarm.HostID); ok {
			memberName = host.Name
		}

		entries = append(entries, domain.AlarmListView{
			ChannelID:  alarm.ChannelID,
			HostID:     alarm.HostID,
			MemberName: memberName,
			AlarmTypes: alarm.AlarmTypes,
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
