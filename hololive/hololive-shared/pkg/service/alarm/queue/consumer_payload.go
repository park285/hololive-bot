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

package queue

import (
	"context"
	"log/slog"
	"strings"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (c *Consumer) appendAcceptedPayloads(
	ctx context.Context,
	source string,
	rawPayloads []string,
	envelopes []domain.AlarmQueueEnvelope,
) []domain.AlarmQueueEnvelope {
	for _, raw := range rawPayloads {
		envelope, ok := parseEnvelope(raw, c.logger)
		if !ok {
			c.moveRawPayloadsToDLQ(ctx, source+"_invalid_payload", []string{raw})
			continue
		}
		if normalized, accepted := c.acceptLegacyEnvelope(ctx, envelope, source); accepted {
			envelopes = append(envelopes, normalized)
		}
	}
	return envelopes
}

func (c *Consumer) moveRawPayloadsToDLQ(ctx context.Context, source string, payloads []string) {
	filtered := make([]string, 0, len(payloads))
	for _, payload := range payloads {
		if payload == "" {
			continue
		}
		filtered = append(filtered, payload)
	}
	if len(filtered) == 0 {
		return
	}

	cmd := c.cache.B().Lpush().Key(c.dlqKey).Element(filtered...).Build()
	results := c.cache.DoMulti(ctx, cmd)
	if len(results) != 1 {
		c.logger.Warn("failed to preserve raw alarm queue payloads to DLQ",
			slog.String("source", source),
			slog.Int("count", len(filtered)),
			slog.Int("result_count", len(results)),
		)
		return
	}
	if err := results[0].Error(); err != nil {
		c.logger.Warn("failed to preserve raw alarm queue payloads to DLQ",
			slog.String("source", source),
			slog.Int("count", len(filtered)),
			slog.Any("error", err),
		)
		return
	}

	alarmQueueDLQMoved.Add(float64(len(filtered)))
	c.logger.Warn("preserved raw alarm queue payloads to DLQ",
		slog.String("source", source),
		slog.Int("count", len(filtered)),
	)
}

func (c *Consumer) acceptLegacyEnvelope(
	ctx context.Context,
	envelope domain.AlarmQueueEnvelope,
	source string,
) (domain.AlarmQueueEnvelope, bool) {
	if envelope.Notification.AlarmType == "" {
		envelope.Notification.AlarmType = domain.AlarmTypeLive
	}
	if err := envelope.Notification.ValidateLegacyRoute(); err != nil {
		alarmQueueEnvelopeTotal.WithLabelValues("rejected_legacy_route").Inc()
		c.logger.Warn("dropping unsupported legacy alarm queue envelope",
			slog.String("source", source),
			slog.String("queue", c.queueKey),
			slog.String("room_id", strings.TrimSpace(envelope.Notification.RoomID)),
			slog.String("alarm_type", string(envelope.Notification.AlarmType)),
			slog.Any("error", err),
		)
		if releaseErr := c.ReleaseClaimKeys(ctx, envelope.ClaimKeys); releaseErr != nil {
			c.logger.Warn("failed to release claim keys for dropped alarm queue envelope",
				slog.String("source", source),
				slog.String("queue", c.queueKey),
				slog.String("room_id", strings.TrimSpace(envelope.Notification.RoomID)),
				slog.Any("error", releaseErr),
			)
		}
		return domain.AlarmQueueEnvelope{}, false
	}

	return envelope, true
}

func parseEnvelope(raw string, logger *slog.Logger) (domain.AlarmQueueEnvelope, bool) {
	initQueueMetrics()

	var envelope domain.AlarmQueueEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		alarmQueueEnvelopeTotal.WithLabelValues("invalid").Inc()
		logger.Warn("failed to parse alarm queue envelope", slog.String("error", err.Error()))
		return domain.AlarmQueueEnvelope{}, false
	}

	switch envelope.Version {
	case 0, contractsalarm.QueueEnvelopeVersionV1:
		alarmQueueEnvelopeTotal.WithLabelValues("accepted").Inc()
		return envelope, true
	default:
		alarmQueueEnvelopeTotal.WithLabelValues("unsupported").Inc()
		logger.Warn("unsupported alarm queue envelope version", slog.Uint64("version", uint64(envelope.Version)))
		return domain.AlarmQueueEnvelope{}, false
	}
}
