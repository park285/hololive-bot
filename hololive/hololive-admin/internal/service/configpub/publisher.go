package configpub

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/service/configsub"
)

// DefaultChannel: configsub과 동일한 Pub/Sub 채널명
const DefaultChannel = configsub.DefaultChannel

// Publisher: Valkey Pub/Sub를 통해 설정 변경을 발행하는 컴포넌트
type Publisher struct {
	client  valkey.Client
	logger  *slog.Logger
	channel string
}

// New: 새로운 Publisher를 생성합니다.
func New(client valkey.Client, logger *slog.Logger) *Publisher {
	return &Publisher{
		client:  client,
		logger:  logger,
		channel: DefaultChannel,
	}
}

// Publish: 설정 변경 메시지를 Pub/Sub 채널에 발행합니다.
func (p *Publisher) Publish(ctx context.Context, update configsub.ConfigUpdate) error {
	jsonBytes, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("marshal config update: %w", err)
	}

	resp := p.client.Do(ctx, p.client.B().Publish().Channel(p.channel).Message(string(jsonBytes)).Build())
	if err := resp.Error(); err != nil {
		return fmt.Errorf("publish config update: %w", err)
	}

	p.logger.Info("Config update published",
		slog.String("type", update.Type),
		slog.String("channel", p.channel),
	)
	return nil
}
