package alarmstore

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
)

// UNIT B의 진행자가 미상이면 멤버별 구독을 모두 포함하며, 구독 조회 실패는 오류로 반환한다.
func (r *SubscriberResolver) ResolveEventSubscribers(
	ctx context.Context,
	channelID, title string,
	alarmType domain.AlarmType,
) ([]string, error) {
	byTitle, err := r.ResolveEventSubscribersByTitle(ctx, channelID, []string{title}, alarmType)
	if err != nil {
		return nil, err
	}

	return byTitle[title], nil
}

// 반환 map의 key는 입력 제목이고, 각 값은 같은 제목을 ResolveEventSubscribers에 넘긴 결과와 같다.
func (r *SubscriberResolver) ResolveEventSubscribersByTitle(
	ctx context.Context,
	channelID string,
	titles []string,
	alarmType domain.AlarmType,
) (map[string][]string, error) {
	channelID = strings.TrimSpace(channelID)

	out := make(map[string][]string, len(titles))

	if !mekparkhost.SupportsSubscriptions(channelID) {
		rooms, err := r.ResolveChannelSubscribersByType(ctx, channelID, alarmType)
		if err != nil {
			return nil, err
		}

		for _, title := range titles {
			out[title] = slices.Clone(rooms)
		}

		return out, nil
	}

	if r.db == nil {
		return nil, errors.New("resolve member subscribers: database is nil")
	}

	queryCtx, cancel := context.WithTimeout(ctx, channelSubscriberLoadTimeout)
	defer cancel()

	alarms, err := newRepositoryWithQuerier(r.db).FindByChannelAndType(queryCtx, channelID, alarmType)
	if err != nil {
		return nil, fmt.Errorf("resolve member subscribers: load channel subscriptions: %w", err)
	}

	for _, title := range titles {
		result := mekparkhost.Identify(channelID, title)
		matching := make([]*domain.Alarm, 0, len(alarms))

		for _, alarm := range alarms {
			if alarm != nil && alarm.ChannelID == channelID && result.MatchesSubscription(alarm.HostID) {
				matching = append(matching, alarm)
			}
		}

		out[title] = extractSubscriberIDsByType(matching, alarmType)
	}

	return out, nil
}
