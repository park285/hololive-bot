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
	"fmt"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/valkey-io/valkey-go"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
)

// Publisher는 설정 변경을 Pub/Sub 채널로 전파합니다.
type Publisher struct {
	client  valkey.Client
	channel string
}

// NewPublisher는 기본 설정 채널을 사용하는 publisher를 생성합니다.
func NewPublisher(client valkey.Client) *Publisher {
	return &Publisher{
		client:  client,
		channel: DefaultChannel,
	}
}

// PublishScraperProxy는 scraper proxy 설정 변경을 발행합니다.
func (p *Publisher) PublishScraperProxy(ctx context.Context, enabled bool) error {
	return p.publish(ctx, contractssettings.UpdateTypeScraperProxy, contractssettings.ScraperProxyPayloadV1{
		Enabled: enabled,
	})
}

// PublishAlarmAdvanceMinutes는 alarm advance minutes 변경을 발행합니다.
func (p *Publisher) PublishAlarmAdvanceMinutes(ctx context.Context, minutes int) error {
	return p.publish(ctx, contractssettings.UpdateTypeAlarmAdvanceMinutes, contractssettings.AlarmAdvanceMinutesPayloadV1{
		Minutes: minutes,
	})
}

func (p *Publisher) publish(ctx context.Context, updateType string, payload any) error {
	if p == nil || p.client == nil {
		return fmt.Errorf("publish config update: client is nil")
	}

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("publish config update: marshal payload: %w", err)
	}

	update := ConfigUpdate{
		Type:    updateType,
		Payload: rawPayload,
	}
	rawUpdate, err := json.Marshal(update)
	if err != nil {
		return fmt.Errorf("publish config update: marshal update: %w", err)
	}

	cmd := p.client.B().Publish().Channel(p.channel).Message(string(rawUpdate)).Build()
	if err := p.client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("publish config update: publish %s: %w", updateType, err)
	}
	return nil
}
