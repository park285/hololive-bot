// Package processinglock: Valkey를 사용한 분산 처리 락 서비스
package processinglock

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/valkey-io/valkey-go"
)

// KeyFunc: chatID를 받아 Valkey 키를 생성하는 함수 타입
type KeyFunc func(chatID string) string

// ErrAlreadyProcessing: 이미 해당 채팅방에서 처리가 진행 중일 때 반환되는 에러
var ErrAlreadyProcessing = errors.New("already processing")

// Service: Valkey를 사용하여 동시 처리를 제어하는 락 서비스
type Service struct {
	client  valkey.Client
	logger  *slog.Logger
	keyFunc KeyFunc
	ttl     time.Duration
}

// New: 새로운 Service 인스턴스를 생성합니다.
func New(client valkey.Client, logger *slog.Logger, keyFunc KeyFunc, ttl time.Duration) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		client:  client,
		logger:  logger,
		keyFunc: keyFunc,
		ttl:     ttl,
	}
}

// Start: 처리 락을 획득합니다. (SET NX)
// 이미 락이 존재하면 ErrAlreadyProcessing 을 반환합니다.
func (s *Service) Start(ctx context.Context, chatID string) error {
	key := s.keyFunc(chatID)
	cmd := s.client.B().Set().Key(key).Value("1").Nx().Ex(s.ttl).Build()
	resp := s.client.Do(ctx, cmd)

	result, err := resp.ToString()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return ErrAlreadyProcessing
		}
		return fmt.Errorf("set processing lock failed: %w", err)
	}

	if result != "OK" {
		return ErrAlreadyProcessing
	}

	s.logger.Debug("processing_started", "chat_id", chatID)
	return nil
}

// Finish: 처리 락을 해제합니다.
func (s *Service) Finish(ctx context.Context, chatID string) error {
	key := s.keyFunc(chatID)
	cmd := s.client.B().Del().Key(key).Build()
	if err := s.client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("delete processing lock failed: %w", err)
	}
	s.logger.Debug("processing_finished", "chat_id", chatID)
	return nil
}

// IsProcessing: 현재 처리가 진행 중인지(락이 존재하는지) 확인합니다.
func (s *Service) IsProcessing(ctx context.Context, chatID string) (bool, error) {
	key := s.keyFunc(chatID)
	cmd := s.client.B().Exists().Key(key).Build()
	n, err := s.client.Do(ctx, cmd).AsInt64()
	if err != nil {
		return false, fmt.Errorf("check processing lock exists failed: %w", err)
	}
	return n > 0, nil
}

// ListLocks: 현재 활성화된 모든 락 키 목록을 반환합니다. (관리 기능)
// keyPattern: SCAN 명령에 사용할 패턴 (예: "assistant:processing:*")
func (s *Service) ListLocks(ctx context.Context, keyPattern string) ([]string, error) {
	var cursor uint64
	var allKeys []string
	const scanCount = 100

	for {
		cmd := s.client.B().Scan().Cursor(cursor).Match(keyPattern).Count(scanCount).Build()
		result := s.client.Do(ctx, cmd)

		entry, err := result.AsScanEntry()
		if err != nil {
			return nil, fmt.Errorf("scan processing locks failed: %w", err)
		}

		allKeys = append(allKeys, entry.Elements...)
		cursor = entry.Cursor

		if cursor == 0 {
			break
		}
	}

	return allKeys, nil
}
