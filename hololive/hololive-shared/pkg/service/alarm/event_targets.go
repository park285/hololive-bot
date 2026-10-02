package alarm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// ResolveEventSubscribers는 제목의 진행자와 알림 종류에 맞는 구독 방을 중복 없이 반환한다.
// UNIT B의 진행자가 미상이면 멤버별 구독을 모두 포함하며, 구독 조회 실패는 오류로 반환한다.
func ResolveEventSubscribers(
	ctx context.Context,
	cacheClient cache.Client,
	db dbx.Querier,
	channelID, title string,
	alarmType domain.AlarmType,
) ([]string, error) {
	byTitle, err := ResolveEventSubscribersByTitle(ctx, cacheClient, db, channelID, []string{title}, alarmType)
	if err != nil {
		return nil, err
	}

	return byTitle[title], nil
}

// ResolveEventSubscribersByTitle는 같은 채널·알림 종류의 여러 제목을 구독 조회 한 번으로 처리한다. 반환 map의 key는
// 입력 제목이고, 각 값은 같은 제목을 ResolveEventSubscribers에 넘긴 결과와 같다. UNIT B 채널은 채널·종류별 구독
// 목록을 한 번 읽은 뒤 제목마다 진행자 필터를 적용하고, 그 밖의 채널은 제목과 무관한 같은 수신 집합을 쓴다.
func ResolveEventSubscribersByTitle(
	ctx context.Context,
	cacheClient cache.Client,
	db dbx.Querier,
	channelID string,
	titles []string,
	alarmType domain.AlarmType,
) (map[string][]string, error) {
	channelID = strings.TrimSpace(channelID)

	out := make(map[string][]string, len(titles))

	if !mekparkhost.SupportsSubscriptions(channelID) {
		rooms, err := ResolveChannelSubscribersByType(ctx, cacheClient, db, channelID, alarmType)
		if err != nil {
			return nil, err
		}

		for _, title := range titles {
			out[title] = slices.Clone(rooms)
		}

		return out, nil
	}

	if db == nil {
		return nil, errors.New("resolve member subscribers: database is nil")
	}

	queryCtx, cancel := context.WithTimeout(ctx, channelSubscriberLoadTimeout)
	defer cancel()

	alarms, err := newRepositoryWithQuerier(db).FindByChannelAndType(queryCtx, channelID, alarmType)
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
