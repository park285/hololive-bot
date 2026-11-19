package dedup

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/privacylog"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type NotifiedData struct {
	StartScheduled string       `json:"start_scheduled"`
	SentAt         map[int]bool `json:"sent_at"`
}

type UpcomingEventNotifiedData struct {
	NotifiedAt string `json:"notified_at"`
}

type LogicalScheduleNotifiedData struct {
	StreamID       string `json:"stream_id"`
	StartScheduled string `json:"start_scheduled"`
	NotifiedAt     string `json:"notified_at"`
}

type Cache interface {
	cache.KeyValueCache
	cache.HashCache
	cache.LowLevelCache
}

type Service struct {
	cache  Cache
	logger *slog.Logger
}

func NewService(c Cache, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	return &Service{
		cache:  c,
		logger: logger,
	}
}

func (s *Service) tryClaimKey(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	acquired, err := s.cache.SetNX(ctx, key, "1", ttl)
	if err != nil {
		return false, fmt.Errorf("dedup claim setnx: claim_key_token=%s: %w", privacylog.Pseudonym(key), err)
	}

	if s.logger.Enabled(ctx, slog.LevelDebug) {
		s.logger.Debug("dedup claim result",
			slog.String("claim_key_token", privacylog.Pseudonym(key)),
			slog.Bool("acquired", acquired),
		)
	}

	return acquired, nil
}
