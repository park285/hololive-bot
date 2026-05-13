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

package youtube

import (
	"context"
	"log/slog"
	"sync/atomic"

	"golang.org/x/sync/errgroup"
)

type alertDispatchResult[T any] struct {
	notification T
	targetRooms  int
	successCount atomic.Int32
	failureCount atomic.Int32
}

func dispatchAlertWorks[T any, W any](
	logger *slog.Logger,
	ctx context.Context,
	sendMessage func(room, message string) error,
	rooms []string,
	works []W,
	notificationOf func(W) T,
	messageOf func(W) string,
	memberNameOf func(T) string,
	sendFailureLog string,
	partialWarnLog string,
) []T {
	if len(works) == 0 || len(rooms) == 0 {
		return nil
	}

	results := newAlertDispatchResults(works, rooms, notificationOf)
	runAlertDispatchWorks(logger, ctx, sendMessage, rooms, works, results, notificationOf, messageOf, memberNameOf, sendFailureLog)
	return collectSentAlertNotifications(logger, results, memberNameOf, partialWarnLog)
}

func newAlertDispatchResults[T any, W any](
	works []W,
	rooms []string,
	notificationOf func(W) T,
) []alertDispatchResult[T] {
	results := make([]alertDispatchResult[T], len(works))
	for i := range works {
		results[i] = alertDispatchResult[T]{
			notification: notificationOf(works[i]),
			targetRooms:  len(rooms),
		}
	}
	return results
}

func runAlertDispatchWorks[T any, W any](
	logger *slog.Logger,
	ctx context.Context,
	sendMessage func(room, message string) error,
	rooms []string,
	works []W,
	results []alertDispatchResult[T],
	notificationOf func(W) T,
	messageOf func(W) string,
	memberNameOf func(T) string,
	sendFailureLog string,
) {
	eg, _ := errgroup.WithContext(ctx)
	eg.SetLimit(4)

	for i := range works {
		work := works[i]
		notification := notificationOf(work)
		message := messageOf(work)
		for _, room := range rooms {
			result := &results[i]
			eg.Go(func() error {
				if err := sendMessage(room, message); err != nil {
					logger.Error(sendFailureLog,
						slog.String("room", room),
						slog.String("member", memberNameOf(notification)),
						slog.Any("error", err))
					result.failureCount.Add(1)
					return nil
				}
				result.successCount.Add(1)
				return nil
			})
		}
	}

	_ = eg.Wait()
}

func collectSentAlertNotifications[T any](
	logger *slog.Logger,
	results []alertDispatchResult[T],
	memberNameOf func(T) string,
	partialWarnLog string,
) []T {
	sentNotifications := make([]T, 0, len(results))
	for i := range results {
		successCount := int(results[i].successCount.Load())
		failureCount := int(results[i].failureCount.Load())
		if results[i].targetRooms > 0 && successCount == results[i].targetRooms && failureCount == 0 {
			sentNotifications = append(sentNotifications, results[i].notification)
			continue
		}
		if successCount > 0 {
			logger.Warn(partialWarnLog,
				slog.String("member", memberNameOf(results[i].notification)),
				slog.Int("target_rooms", results[i].targetRooms),
				slog.Int("success_count", successCount),
				slog.Int("failure_count", failureCount))
		}
	}

	return sentNotifications
}
