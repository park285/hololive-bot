package alarmstore

import (
	"context"
	"fmt"
	"slices"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/kakaoroom"
)

type LiveRooms struct {
	subscribers *SubscriberResolver
	pauses      *kakaoroom.DeliveryPauses
}

func NewLiveRooms(subscribers *SubscriberResolver, pauses *kakaoroom.DeliveryPauses) *LiveRooms {
	return &LiveRooms{subscribers: subscribers, pauses: pauses}
}

func (l *LiveRooms) LiveRooms(ctx context.Context, channelID string) ([]string, error) {
	subscribers, err := l.subscribers.ResolveChannelSubscribersByType(ctx, channelID, domain.AlarmTypeLive)
	if err != nil {
		return nil, fmt.Errorf("live rooms: %w", err)
	}

	rooms := slices.Compact(slices.Sorted(slices.Values(subscribers)))
	if len(rooms) == 0 {
		return []string{}, nil
	}

	pauses, err := l.pauses.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("live rooms: %w", err)
	}

	return slices.DeleteFunc(rooms, func(room string) bool {
		return slices.ContainsFunc(pauses, func(pause kakaoroom.DeliveryPause) bool { return pause.RoomID == room })
	}), nil
}
