// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package configsub

import (
	"context"
	"errors"
	"log/slog"

	"github.com/park285/shared-go/pkg/json"
	"github.com/valkey-io/valkey-go"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
)

const DefaultChannel = contractssettings.PubSubChannelV1

type ConfigUpdate struct {
	Type    string          `json:"type"`    // contracts/settings UpdateType* 상수 사용
	Payload json.RawMessage `json:"payload"` // 타입별 페이로드 (JSON)
}

type Subscriber struct {
	client  valkey.Client
	applyFn func(ConfigUpdate)
	logger  *slog.Logger
	channel string
}

func New(client valkey.Client, applyFn func(ConfigUpdate), logger *slog.Logger) *Subscriber {
	return &Subscriber{
		client:  client,
		applyFn: applyFn,
		logger:  logger,
		channel: DefaultChannel,
	}
}

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
