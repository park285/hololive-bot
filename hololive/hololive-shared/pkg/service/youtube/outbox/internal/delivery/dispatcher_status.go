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

package delivery

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (d *Dispatcher) markSent(ctx context.Context, id int64) {
	d.statusUpdater().markSent(ctx, id)
}

func (d *Dispatcher) markSentBatch(ctx context.Context, ids []int64) {
	d.statusUpdater().markSentBatch(ctx, ids)
}

func (d *Dispatcher) markFailed(ctx context.Context, id int64, errMsg string) {
	d.statusUpdater().markFailed(ctx, id, errMsg)
}

func (d *Dispatcher) markFailedPermanently(ctx context.Context, id int64, attemptCount int, errMsg string) {
	d.statusUpdater().markFailedPermanently(ctx, id, attemptCount, errMsg)
}

func (d *Dispatcher) scheduleFailedRetry(ctx context.Context, id int64, attemptCount int, errMsg string) {
	d.statusUpdater().scheduleFailedRetry(ctx, id, attemptCount, errMsg)
}

func (d *Dispatcher) statusUpdater() *StatusUpdater {
	if d == nil {
		return newStatusUpdater(nil, nil, Config{})
	}
	if d.status != nil {
		return d.status
	}
	return newStatusUpdater(nil, d.logger, d.config)
}

func collectOutboxIDs(items []domain.YouTubeNotificationOutbox) []int64 {
	if len(items) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(items))
	for i := range items {
		ids = append(ids, items[i].ID)
	}
	return ids
}

func uniqueInt64s(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	unique := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

// truncateString: 문자열 길이 제한
func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}
