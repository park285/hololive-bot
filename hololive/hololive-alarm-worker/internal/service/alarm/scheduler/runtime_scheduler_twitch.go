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

package scheduler

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/panicguard"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
	"golang.org/x/sync/errgroup"
)

func newOptionalTwitchChecker(
	cacheClient cache.Client,
	twitchClient *twitch.Client,
	twitchEnabled bool,
	logger *slog.Logger,
) (checker.Runner, error) {
	if !twitchEnabled {
		logger.Info("Twitch alarm loop disabled")
		return nil, nil
	}
	twitchChecker, err := checker.NewTwitchChecker(cacheClient, twitchClient, logger)
	if err != nil {
		return nil, fmt.Errorf("new runtime scheduler: create twitch checker: %w", err)
	}
	return twitchChecker, nil
}

func (s *RuntimeScheduler) startTwitchLoop(eg *errgroup.Group, ctx context.Context) {
	if s.twitchChecker == nil {
		return
	}
	panicguard.GoE(eg, s.logger, "alarm-scheduler-twitch", func() error {
		return s.runLoop(ctx, runtimeSchedulerLoopNameTwitch, s.twitchInterval, s.twitchTimeout, true, s.runTwitchIteration)
	})
}
