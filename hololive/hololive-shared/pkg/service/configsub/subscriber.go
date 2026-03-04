package configsub

import (
	"context"
	"errors"
	"log/slog"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/valkey-io/valkey-go"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
)

// DefaultChannel: 설정 변경 Pub/Sub 채널 기본 이름
const DefaultChannel = contractssettings.PubSubChannelV1

// ConfigUpdate: Pub/Sub로 전달되는 설정 변경 메시지
type ConfigUpdate struct {
	Type    string          `json:"type"`    // contracts/settings UpdateType* 상수 사용
	Payload json.RawMessage `json:"payload"` // 타입별 페이로드 (JSON)
}

// Subscriber: Valkey Pub/Sub를 통해 설정 변경을 수신하는 컴포넌트
type Subscriber struct {
	client  valkey.Client
	applyFn func(ConfigUpdate)
	logger  *slog.Logger
	channel string
}

// New: 새로운 ConfigSubscriber를 생성합니다.
func New(client valkey.Client, applyFn func(ConfigUpdate), logger *slog.Logger) *Subscriber {
	return &Subscriber{
		client:  client,
		applyFn: applyFn,
		logger:  logger,
		channel: DefaultChannel,
	}
}

// Run: 블로킹으로 Pub/Sub 메시지를 수신합니다. goroutine으로 실행해야 합니다.
// ctx가 취소되면 종료합니다.
func (s *Subscriber) Run(ctx context.Context) {
	s.logger.Info("Config subscriber started", slog.String("channel", s.channel))

	err := s.client.Receive(ctx, s.client.B().Subscribe().Channel(s.channel).Build(), func(msg valkey.PubSubMessage) {
		var update ConfigUpdate
		if err := json.Unmarshal([]byte(msg.Message), &update); err != nil {
			s.logger.Warn("Failed to unmarshal config update",
				slog.String("channel", s.channel),
				slog.Any("error", err),
			)
			return
		}

		if update.Type == "" {
			s.logger.Warn("Config update with empty type, ignoring",
				slog.String("channel", s.channel),
			)
			return
		}

		s.logger.Info("Config update received",
			slog.String("type", update.Type),
			slog.String("channel", s.channel),
		)
		s.applyFn(update)
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Error("Config subscriber error",
			slog.String("channel", s.channel),
			slog.Any("error", err),
		)
	}

	s.logger.Info("Config subscriber stopped", slog.String("channel", s.channel))
}
