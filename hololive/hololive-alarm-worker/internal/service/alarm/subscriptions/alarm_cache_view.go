package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
)

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

	memberNames, err := as.memberDisplayNames(ctx, channelIDs)
	if err != nil {
		opErr = fmt.Errorf("list room alarms view: %w", err)
		return nil, opErr
	}

	return buildAlarmListViews(alarms, memberNames), nil
}

// memberDisplayNames는 채널별 멤버 표시명을 members 정본에서 domain.Member.DisplayName 순서
// (short_korean_name→korean_name→english_name)로 읽는다. 멤버 정본에 없는 채널은 빈 이름이다. 조회 오류는 다른 이름으로
// 덮지 않고 돌려준다. 같은 채널은 한 번만 조회한다.
func (as *AlarmService) memberDisplayNames(ctx context.Context, channelIDs []string) (map[string]string, error) {
	if as.memberData == nil {
		return nil, errors.New("member display names: member data provider is nil")
	}

	names := make(map[string]string, len(channelIDs))

	for _, channelID := range channelIDs {
		if _, resolved := names[channelID]; resolved {
			continue
		}

		member, err := as.memberData.FindMemberByChannelID(ctx, channelID)

		switch {
		case errors.Is(err, domain.ErrMemberNotFound):
			names[channelID] = ""
		case err != nil:
			return nil, fmt.Errorf("member display names: find member by channel: %w", err)
		default:
			names[channelID] = member.DisplayName()
		}
	}

	return names, nil
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
