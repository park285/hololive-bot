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

package milestonescheduler

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

type alertDispatchResult[T any] struct {
	notification T
	key          string
	targetRooms  int
	successCount atomic.Int32
	failureCount atomic.Int32
}

type sentRoomLedger struct {
	mu    sync.Mutex
	byKey map[string]map[string]struct{}
}

func (l *sentRoomLedger) alreadySent(key, room string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.byKey[key][room]
	return ok
}

func (l *sentRoomLedger) recordSent(key, room string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byKey == nil {
		l.byKey = make(map[string]map[string]struct{})
	}
	rooms := l.byKey[key]
	if rooms == nil {
		rooms = make(map[string]struct{})
		l.byKey[key] = rooms
	}
	rooms[room] = struct{}{}
}

func (l *sentRoomLedger) sentCount(key string) int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.byKey[key])
}

func (l *sentRoomLedger) clear(key string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byKey, key)
}

func dispatchAlertWorks[T any, W any](
	logger *slog.Logger,
	ctx context.Context,
	ledger *sentRoomLedger,
	sendMessage func(room, message string) error,
	rooms []string,
	works []W,
	notificationOf func(W) T,
	messageOf func(W) string,
	keyOf func(W) string,
	memberNameOf func(T) string,
	sendFailureLog string,
	partialWarnLog string,
) []T {
	if len(works) == 0 || len(rooms) == 0 {
		return nil
	}

	results := newAlertDispatchResults(works, rooms, notificationOf, keyOf)
	runAlertDispatchWorks(logger, ctx, ledger, sendMessage, rooms, works, results, notificationOf, messageOf, keyOf, memberNameOf, sendFailureLog)
	return collectSentAlertNotifications(logger, ledger, results, memberNameOf, partialWarnLog)
}

func newAlertDispatchResults[T any, W any](
	works []W,
	rooms []string,
	notificationOf func(W) T,
	keyOf func(W) string,
) []alertDispatchResult[T] {
	results := make([]alertDispatchResult[T], len(works))
	for i := range works {
		results[i] = alertDispatchResult[T]{
			notification: notificationOf(works[i]),
			key:          keyOf(works[i]),
			targetRooms:  len(rooms),
		}
	}
	return results
}

func runAlertDispatchWorks[T any, W any](
	logger *slog.Logger,
	ctx context.Context,
	ledger *sentRoomLedger,
	sendMessage func(room, message string) error,
	rooms []string,
	works []W,
	results []alertDispatchResult[T],
	notificationOf func(W) T,
	messageOf func(W) string,
	keyOf func(W) string,
	memberNameOf func(T) string,
	sendFailureLog string,
) {
	eg, _ := errgroup.WithContext(ctx)
	eg.SetLimit(4)

	for i := range works {
		work := works[i]
		scheduleAlertWorkDispatch(eg, logger, ledger, sendMessage, rooms, &results[i],
			notificationOf(work), messageOf(work), keyOf(work), memberNameOf, sendFailureLog)
	}

	if err := eg.Wait(); err != nil {
		logger.Warn("Alert dispatch worker failed", slog.Any("error", err))
	}
}

func scheduleAlertWorkDispatch[T any](
	eg *errgroup.Group,
	logger *slog.Logger,
	ledger *sentRoomLedger,
	sendMessage func(room, message string) error,
	rooms []string,
	result *alertDispatchResult[T],
	notification T,
	message string,
	key string,
	memberNameOf func(T) string,
	sendFailureLog string,
) {
	for _, room := range rooms {
		if ledger.alreadySent(key, room) {
			continue
		}
		eg.Go(func() error {
			if err := sendMessage(room, message); err != nil {
				logger.Error(sendFailureLog,
					slog.String("room", room),
					slog.String("member", memberNameOf(notification)),
					slog.Any("error", err))
				result.failureCount.Add(1)
				return nil
			}
			ledger.recordSent(key, room)
			result.successCount.Add(1)
			return nil
		})
	}
}

func collectSentAlertNotifications[T any](
	logger *slog.Logger,
	ledger *sentRoomLedger,
	results []alertDispatchResult[T],
	memberNameOf func(T) string,
	partialWarnLog string,
) []T {
	sentNotifications := make([]T, 0, len(results))
	for i := range results {
		successCount := int(results[i].successCount.Load())
		failureCount := int(results[i].failureCount.Load())
		coveredRooms := ledger.sentCount(results[i].key)
		if results[i].targetRooms > 0 && coveredRooms >= results[i].targetRooms {
			sentNotifications = append(sentNotifications, results[i].notification)
			ledger.clear(results[i].key)
			continue
		}
		if successCount > 0 || failureCount > 0 {
			logger.Warn(partialWarnLog,
				slog.String("member", memberNameOf(results[i].notification)),
				slog.Int("target_rooms", results[i].targetRooms),
				slog.Int("covered_rooms", coveredRooms),
				slog.Int("success_count", successCount),
				slog.Int("failure_count", failureCount))
		}
	}

	return sentNotifications
}
