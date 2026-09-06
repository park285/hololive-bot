package alarm

import (
	"context"
	"errors"
	"fmt"
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
	channelID = strings.TrimSpace(channelID)
	if !mekparkhost.SupportsSubscriptions(channelID) {
		return ResolveChannelSubscribersByType(ctx, cacheClient, db, channelID, alarmType)
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

	result := mekparkhost.Identify(channelID, title)
	matching := make([]*domain.Alarm, 0, len(alarms))

	for _, alarm := range alarms {
		if alarm != nil && alarm.ChannelID == channelID && result.MatchesSubscription(alarm.HostID) {
			matching = append(matching, alarm)
		}
	}

	return extractSubscriberIDsByType(matching, alarmType), nil
}
