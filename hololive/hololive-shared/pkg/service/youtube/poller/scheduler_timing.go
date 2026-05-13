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

package poller

import (
	"crypto/sha256"
	"encoding/binary"
	"time"
)

func nextErrorRetryAt(now time.Time, interval time.Duration, consecutiveFailures int, minBackoff, maxBackoff time.Duration) time.Time {
	delay := errorRetryDelay(interval, consecutiveFailures, minBackoff, maxBackoff)
	return now.Add(delay)
}

func errorRetryDelay(interval time.Duration, consecutiveFailures int, minBackoff, maxBackoff time.Duration) time.Duration {
	minBackoff, maxBackoff = normalizeRetryBackoffBounds(minBackoff, maxBackoff)

	if consecutiveFailures <= 1 {
		return capRetryDelayByInterval(minBackoff, interval)
	}

	delay := exponentialRetryBackoff(minBackoff, maxBackoff, consecutiveFailures)
	delay = min(capRetryDelayByInterval(delay, interval), maxBackoff)
	return delay
}

func normalizeRetryBackoffBounds(minBackoff, maxBackoff time.Duration) (time.Duration, time.Duration) {
	if minBackoff <= 0 {
		minBackoff = 30 * time.Second
	}
	if maxBackoff <= 0 {
		maxBackoff = 5 * time.Minute
	}
	if maxBackoff < minBackoff {
		maxBackoff = minBackoff
	}
	return minBackoff, maxBackoff
}

func exponentialRetryBackoff(minBackoff, maxBackoff time.Duration, consecutiveFailures int) time.Duration {
	delay := minBackoff
	for i := 1; i < consecutiveFailures; i++ {
		if delay >= maxBackoff/2 {
			delay = maxBackoff
			break
		}
		delay *= 2
	}
	return delay
}

func capRetryDelayByInterval(delay, interval time.Duration) time.Duration {
	if interval > 0 && interval < delay {
		return interval
	}
	return delay
}

func advanceNextRunAt(scheduledAt time.Time, interval time.Duration, now time.Time) time.Time {
	if interval <= 0 {
		return now
	}
	if scheduledAt.IsZero() {
		return now
	}
	if scheduledAt.After(now) {
		return scheduledAt
	}

	skipped := now.Sub(scheduledAt)/interval + 1
	return scheduledAt.Add(time.Duration(int64(skipped) * interval.Nanoseconds()))
}

func nextPollAt(now time.Time, interval, offset time.Duration) time.Time {
	if interval <= 0 {
		return now
	}

	if offset < 0 {
		offset = 0
	}
	if offset >= interval {
		offset %= interval
	}

	next := now.Truncate(interval).Add(offset)
	if next.After(now) {
		return next
	}

	return next.Add(interval)
}

func calculateOffset(key string, interval time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	h := sha256.Sum256([]byte(key))
	fraction := float64(binary.BigEndian.Uint32(h[:4])) / float64(^uint32(0))
	return time.Duration(float64(interval) * fraction)
}
