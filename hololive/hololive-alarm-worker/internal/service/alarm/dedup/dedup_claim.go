package dedup

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

// 두 키를 한 pipeline으로 SetNX하면 경쟁자끼리 키를 나눠 잡은 뒤 서로 release해
// 승자 0명이 될 수 있다(중복 배치에서 알림 전량 skip). 그래서 key1 승자만 key2를 시도한다.
// 두 번째 key의 저장소 오류에서는 acquired1=true를 함께 돌려줘 호출자가 key1을 release할 수 있게 한다.
func (s *Service) TryClaimPair(ctx context.Context, key1, key2 string, ttl time.Duration) (acquired1, acquired2 bool, err error) {
	acquired1, err = s.tryClaimKey(ctx, key1, ttl)
	if err != nil {
		return false, false, fmt.Errorf("try claim pair: first key: %w", err)
	}

	if !acquired1 {
		return false, false, nil
	}

	acquired2, err = s.tryClaimKey(ctx, key2, ttl)
	if err != nil {
		return true, false, fmt.Errorf("try claim pair: second key: %w", err)
	}

	return true, acquired2, nil
}

func (s *Service) TryClaimRoomScheduleTransition(ctx context.Context, roomID, streamID string, oldScheduled, newScheduled time.Time) (string, bool, error) {
	key := keys.BuildRoomScheduleTransitionKey(roomID, streamID, oldScheduled, newScheduled)

	acquired, err := s.tryClaimKey(ctx, key, constants.CacheTTL.NotificationSent)
	if err != nil {
		return key, false, fmt.Errorf("try claim room schedule transition: %w", err)
	}

	return key, acquired, nil
}

func (s *Service) TryClaimLogicalScheduleTransition(ctx context.Context, roomID, channelID string, stream *domain.Stream, oldScheduled, newScheduled time.Time) (string, bool, error) {
	if stream == nil {
		return "", false, nil
	}

	key := keys.BuildLogicalScheduleTransitionKey(roomID, channelID, stream.ID, stream.Title, oldScheduled, newScheduled)

	acquired, err := s.tryClaimKey(ctx, key, constants.CacheTTL.NotificationSent)
	if err != nil {
		return key, false, fmt.Errorf("try claim logical schedule transition: %w", err)
	}

	return key, acquired, nil
}

func (s *Service) ReleaseClaims(ctx context.Context, claimKeys []string) error {
	if len(claimKeys) == 0 {
		return nil
	}

	_, err := s.cache.DelMany(ctx, claimKeys)
	if err != nil {
		return fmt.Errorf("release claims: del many keys: %w", err)
	}

	return nil
}
