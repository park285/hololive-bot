package processinglock

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/valkey-io/valkey-go"
)

type DomainService struct {
	service *Service
}

func NewDomainService(
	client valkey.Client,
	logger *slog.Logger,
	keyFunc KeyFunc,
	ttl time.Duration,
) *DomainService {
	return &DomainService{
		service: New(client, logger, keyFunc, ttl),
	}
}

func (s *DomainService) StartProcessing(ctx context.Context, chatID string) error {
	if err := s.service.Start(ctx, chatID); err != nil {
		return fmt.Errorf("start processing failed: %w", err)
	}
	return nil
}

func (s *DomainService) FinishProcessing(ctx context.Context, chatID string) error {
	if err := s.service.Finish(ctx, chatID); err != nil {
		return fmt.Errorf("finish processing failed: %w", err)
	}
	return nil
}

func (s *DomainService) IsProcessing(ctx context.Context, chatID string) (bool, error) {
	processing, err := s.service.IsProcessing(ctx, chatID)
	if err != nil {
		return false, fmt.Errorf("check processing failed: %w", err)
	}
	return processing, nil
}
