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
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/kapu/hololive-shared/pkg/util"
)

const drainDelayedRetriesScript = `
local members = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, ARGV[2])
if #members == 0 then
  return members
end
redis.call('ZREM', KEYS[1], unpack(members))
return members`

func (c *Consumer) drainDelayedRetries(ctx context.Context, count int, now time.Time) ([]string, error) {
	if count <= 0 {
		return nil, nil
	}

	cmd := c.cache.B().Eval().
		Script(drainDelayedRetriesScript).
		Numkeys(1).
		Key(c.retryQueueKey).
		Arg(strconv.FormatInt(now.UnixMilli(), 10), strconv.Itoa(count)).
		Build()
	results := c.cache.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return nil, fmt.Errorf("drain delayed retry payloads: unexpected result count: %d", len(results))
	}
	values, err := results[0].AsStrSlice()
	if err != nil {
		if util.IsValkeyNil(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("drain delayed retry payloads: execute script: %w", err)
	}
	if len(values) > 0 {
		alarmQueueRetryDrained.Add(float64(len(values)))
	}
	payloads := make([]string, 0, len(values))
	invalidMembers := make([]string, 0)
	for _, value := range values {
		payload, err := unwrapRetryMember(value)
		if err != nil {
			invalidMembers = append(invalidMembers, value)
			c.logger.Warn("invalid delayed retry member wrapper; preserving raw member to DLQ",
				slog.String("retry_queue", c.retryQueueKey),
				slog.Any("error", err),
			)
			continue
		}
		payloads = append(payloads, payload)
	}
	if len(invalidMembers) > 0 {
		c.moveRawPayloadsToDLQ(ctx, "drain_delayed_invalid_member", invalidMembers)
	}
	return payloads, nil
}

// brpop: Valkey BRPOP 래퍼
