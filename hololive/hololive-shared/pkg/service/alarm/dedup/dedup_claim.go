package dedup

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// startScheduled가 zero이면 ("", false, nil) 반환
func (s *Service) TryClaimNotification(ctx context.Context, roomID, streamID string, startScheduled time.Time, minutesUntil int) (value0 string, ok1 bool, err error) {
	if startScheduled.IsZero() {
		return "", false, nil
	}

	category := keys.NotificationCategory(s.targetMinutesSnapshot(), minutesUntil)
	key := keys.BuildNotifyClaimKey(roomID, streamID, startScheduled, category)
	acquired := s.tryClaimKey(ctx, key, constants.CacheTTL.NotificationSent)
	return key, acquired, nil
}

func (s *Service) TryClaimLogicalEvent(ctx context.Context, roomID, channelID string, stream *domain.Stream, minutesUntil int) (value0 string, ok1 bool, err error) {
	if stream == nil {
		return "", false, nil
	}

	if stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return "", false, nil
	}

	category := keys.NotificationCategory(s.targetMinutesSnapshot(), minutesUntil)
	key := keys.BuildLogicalEventClaimKey(roomID, channelID, stream.ID, stream.Title, *stream.StartScheduled, category)
	acquired := s.tryClaimKey(ctx, key, constants.CacheTTL.NotificationSent)
	return key, acquired, nil
}

func (s *Service) TryClaimPair(ctx context.Context, key1, key2 string, ttl time.Duration) (acquired1, acquired2 bool) {
	results, err := s.cache.SetNXMulti(ctx, []cache.SetNXEntry{
		{Key: key1, Value: "1", TTL: ttl},
		{Key: key2, Value: "1", TTL: ttl},
	})
	if err != nil {
		return s.fallback.TryClaimOnOutage(key1, ttl, err),
			s.fallback.TryClaimOnOutage(key2, ttl, err)
	}
	if len(results) != 2 {
		err := fmt.Errorf("setnx multi: unexpected result count: %d", len(results))
		return s.fallback.TryClaimOnOutage(key1, ttl, err),
			s.fallback.TryClaimOnOutage(key2, ttl, err)
	}
	return s.resolveClaimResult(key1, ttl, results[0]),
		s.resolveClaimResult(key2, ttl, results[1])
}

func (s *Service) resolveClaimResult(key string, ttl time.Duration, r cache.SetNXResult) bool {
	if r.Err != nil {
		return s.fallback.TryClaimOnOutage(key, ttl, r.Err)
	}
	return r.Acquired
}

func (s *Service) TryClaimScheduleTransition(ctx context.Context, streamID string, oldScheduled, newScheduled time.Time) (value0 string, ok1 bool, err error) {
	key := keys.BuildScheduleTransitionKey(streamID, oldScheduled, newScheduled)
	acquired := s.tryClaimKey(ctx, key, constants.CacheTTL.NotificationSent)
	return key, acquired, nil
}

func (s *Service) TryClaimRoomScheduleTransition(ctx context.Context, roomID, streamID string, oldScheduled, newScheduled time.Time) (value0 string, ok1 bool, err error) {
	key := keys.BuildRoomScheduleTransitionKey(roomID, streamID, oldScheduled, newScheduled)
	acquired := s.tryClaimKey(ctx, key, constants.CacheTTL.NotificationSent)
	return key, acquired, nil
}

func (s *Service) TryClaimLogicalScheduleTransition(ctx context.Context, roomID, channelID string, stream *domain.Stream, oldScheduled, newScheduled time.Time) (value0 string, ok1 bool, err error) {
	if stream == nil {
		return "", false, nil
	}

	key := keys.BuildLogicalScheduleTransitionKey(roomID, channelID, stream.ID, stream.Title, oldScheduled, newScheduled)
	acquired := s.tryClaimKey(ctx, key, constants.CacheTTL.NotificationSent)
	return key, acquired, nil
}

func (s *Service) ReleaseClaims(ctx context.Context, claimKeys []string) error {
	if len(claimKeys) == 0 {
		return nil
	}
	s.fallback.ReleaseClaims(claimKeys)

	_, err := s.cache.DelMany(ctx, claimKeys)
	if err != nil {
		return fmt.Errorf("release claims: del many keys: %w", err)
	}
	return nil
}
