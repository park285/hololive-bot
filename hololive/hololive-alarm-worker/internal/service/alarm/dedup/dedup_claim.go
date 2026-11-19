package dedup

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const NotificationSentTTL = 24 * time.Hour

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

	acquired, err := s.tryClaimKey(ctx, key, NotificationSentTTL)
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

	acquired, err := s.tryClaimKey(ctx, key, NotificationSentTTL)
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
