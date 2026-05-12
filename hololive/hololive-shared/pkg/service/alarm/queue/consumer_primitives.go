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
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

func (c *Consumer) brpop(ctx context.Context, timeout time.Duration) (string, error) {
	cmd := c.cache.B().Brpop().Key(c.queueKey).Timeout(timeout.Seconds()).Build()
	results := c.cache.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return "", fmt.Errorf("brpop queue payload: unexpected result count: %d", len(results))
	}
	result, err := results[0].AsStrSlice()
	if err != nil {
		if util.IsValkeyNil(err) {
			return "", nil
		}
		return "", fmt.Errorf("brpop queue payload: execute command: %w", err)
	}

	// BRPOP은 [key, value] 쌍 반환
	if len(result) < 2 {
		return "", nil
	}
	return result[1], nil
}

func (c *Consumer) rpopMany(ctx context.Context, count int) ([]string, error) {
	if count <= 0 {
		return nil, nil
	}

	cmd := c.cache.B().Rpop().Key(c.queueKey).Count(int64(count)).Build()
	results := c.cache.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return nil, fmt.Errorf("rpop queue payloads: unexpected result count: %d", len(results))
	}
	values, err := results[0].AsStrSlice()
	if err != nil {
		if util.IsValkeyNil(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("rpop queue payloads: execute command: %w", err)
	}
	return values, nil
}

func resolveRetryVisibleAt(envelope domain.AlarmQueueEnvelope, now time.Time) (time.Time, error) {
	if envelope.Retry == nil {
		return time.Time{}, fmt.Errorf("retry metadata is required")
	}
	if trimmed := strings.TrimSpace(envelope.Retry.NextVisibleAt); trimmed != "" {
		nextVisibleAt, err := time.Parse(time.RFC3339Nano, trimmed)
		if err != nil {
			return time.Time{}, fmt.Errorf("parse retry next_visible_at: %w", err)
		}
		return nextVisibleAt.UTC(), nil
	}
	if envelope.Retry.RetryAfterMS < 0 {
		return time.Time{}, fmt.Errorf("retry_after_ms must be zero or greater")
	}
	return now.Add(time.Duration(envelope.Retry.RetryAfterMS) * time.Millisecond), nil
}

func deriveRetryQueueKey(queueKey string) string {
	base := strings.TrimSpace(queueKey)
	if base == "" {
		base = contractsalarm.DispatchQueueKey
	}
	if strings.HasSuffix(base, ":queue") {
		return strings.TrimSuffix(base, ":queue") + ":retry"
	}
	return base + ":retry"
}

func deriveDLQKey(queueKey string) string {
	base := strings.TrimSpace(queueKey)
	if base == "" {
		base = contractsalarm.DispatchQueueKey
	}
	if strings.HasSuffix(base, ":queue") {
		return strings.TrimSuffix(base, ":queue") + ":dlq"
	}
	return base + ":dlq"
}

func shouldPreferOriginalPayload(envelope domain.AlarmQueueEnvelope, currentPayload string) bool {
	originalPayload := envelope.OriginalPayload()
	if originalPayload == "" {
		return false
	}
	return currentPayload == envelope.NormalizedPayload()
}

func buildRetryMember(nextVisibleAtMillis int64, batchToken uint64, index int, payload string) string {
	return fmt.Sprintf(
		"%s%013d:%020d:%06d:%s",
		retryMemberPrefix,
		nextVisibleAtMillis,
		batchToken,
		index,
		base64.RawStdEncoding.EncodeToString([]byte(payload)),
	)
}

func unwrapRetryMember(member string) (string, error) {
	if !strings.HasPrefix(member, retryMemberPrefix) {
		return member, nil
	}

	parts := strings.SplitN(strings.TrimPrefix(member, retryMemberPrefix), ":", 4)
	if len(parts) != 4 {
		return "", fmt.Errorf("invalid retry member wrapper")
	}

	payload, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return "", fmt.Errorf("decode retry member payload: %w", err)
	}
	return string(payload), nil
}

// parseEnvelope: JSON을 AlarmQueueEnvelope로 파싱 (v0/v1 지원)
