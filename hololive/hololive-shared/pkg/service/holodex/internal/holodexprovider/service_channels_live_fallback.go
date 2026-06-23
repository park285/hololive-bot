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

package holodexprovider

import (
	"context"
	stdErrors "errors"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/livestatus"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type channelsLiveStatusFallbackResult struct {
	streams  []*domain.Stream
	failed   map[string]error
	deferred map[string]error
}

func (h *Service) getChannelsLiveStatusFromScraper(ctx context.Context, channelIDs []string) channelsLiveStatusFallbackResult {
	result := channelsLiveStatusFallbackResult{
		streams: make([]*domain.Stream, 0, len(channelIDs)),
	}
	if len(channelIDs) == 0 {
		return result
	}

	cfg := h.effectiveLiveStatusFallbackConfig()
	fallbackCtx, cancel, err := h.liveStatusFallbackContext(ctx, cfg)
	if err != nil {
		result.deferred = deferAllChannels(channelIDs, livestatus.DeferredReasonContextDone, err)
		return result
	}
	defer cancel()

	selectedSet := h.fetchLiveStatusFallbackSelection(fallbackCtx, channelIDs, cfg.MaxPerCycle, &result)
	for channelID, err := range deferUnselectedChannels(channelIDs, selectedSet) {
		result.deferred = putChannelError(result.deferred, channelID, err)
	}

	return result
}

func (h *Service) fetchLiveStatusFallbackSelection(
	ctx context.Context,
	channelIDs []string,
	maxPerCycle int,
	result *channelsLiveStatusFallbackResult,
) map[string]struct{} {
	selectedSet := make(map[string]struct{}, min(maxPerCycle, len(channelIDs)))
	for attempts := 0; attempts < maxPerCycle && attempts < len(channelIDs); attempts++ {
		if err := ctx.Err(); err != nil {
			break
		}

		channelID := h.nextLiveStatusFallbackChannel(channelIDs)
		selectedSet[channelID] = struct{}{}
		h.fetchLiveStatusFallbackChannel(ctx, channelID, result)
	}
	return selectedSet
}

func (h *Service) fetchLiveStatusFallbackChannel(ctx context.Context, channelID string, result *channelsLiveStatusFallbackResult) {
	streams, err := h.scraper.FetchFromYouTubeProducerWaitAdmission(ctx, channelID)
	if err != nil {
		recordLiveStatusFallbackChannelError(result, channelID, err)
		return
	}
	result.streams = append(result.streams, streams...)
}

func recordLiveStatusFallbackChannelError(result *channelsLiveStatusFallbackResult, channelID string, err error) {
	reason, deferred := classifyLiveStatusFallbackDeferredError(err)
	if deferred {
		result.deferred = putChannelError(result.deferred, channelID, livestatus.NewDeferred(reason, channelID, err))
		return
	}
	result.failed = putChannelError(result.failed, channelID, err)
}

func (h *Service) nextLiveStatusFallbackChannel(channelIDs []string) string {
	if len(channelIDs) == 0 {
		return ""
	}
	h.liveFallbackMu.Lock()
	defer h.liveFallbackMu.Unlock()

	index := h.liveFallbackCursor % len(channelIDs)
	h.liveFallbackCursor = (index + 1) % len(channelIDs)
	return channelIDs[index]
}

func (h *Service) liveStatusFallbackContext(ctx context.Context, cfg config.HolodexLiveStatusFallbackConfig) (context.Context, context.CancelFunc, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	now := time.Now()
	deadline := now.Add(cfg.WallClockBudget)
	if parentDeadline, ok := ctx.Deadline(); ok {
		parentBudgetDeadline := parentDeadline.Add(-cfg.DeadlineMargin)
		if parentBudgetDeadline.Before(deadline) {
			deadline = parentBudgetDeadline
		}
	}
	if !deadline.After(now) {
		return nil, nil, context.DeadlineExceeded
	}
	fallbackCtx, cancel := context.WithDeadline(ctx, deadline)
	return fallbackCtx, cancel, nil
}

func putChannelError(target map[string]error, channelID string, err error) map[string]error {
	if err == nil {
		return target
	}
	if target == nil {
		target = make(map[string]error, 1)
	}
	target[channelID] = err
	return target
}

func deferAllChannels(channelIDs []string, reason livestatus.DeferredReason, cause error) map[string]error {
	deferred := make(map[string]error, len(channelIDs))
	for _, channelID := range channelIDs {
		deferred[channelID] = livestatus.NewDeferred(reason, channelID, cause)
	}
	return deferred
}

func deferUnselectedChannels(channelIDs []string, selected map[string]struct{}) map[string]error {
	if len(channelIDs) == 0 {
		return nil
	}
	deferred := make(map[string]error)
	for _, channelID := range channelIDs {
		if _, ok := selected[channelID]; ok {
			continue
		}
		deferred[channelID] = livestatus.NewDeferred(livestatus.DeferredReasonPerCycleCap, channelID, nil)
	}
	if len(deferred) == 0 {
		return nil
	}
	return deferred
}

type liveStatusFallbackDeferredRule struct {
	reason livestatus.DeferredReason
	match  func(error) bool
}

var liveStatusFallbackDeferredRules = []liveStatusFallbackDeferredRule{
	{reason: livestatus.DeferredReasonContextDone, match: isLiveStatusFallbackContextDone},
	{reason: livestatus.DeferredReasonYouTubeCooldown, match: isLiveStatusFallbackYouTubeCooldown},
	{reason: livestatus.DeferredReasonAdmissionDeferred, match: scraper.IsAdmissionDeferred},
	{reason: livestatus.DeferredReasonDistributedLimiterUnavailable, match: scraper.IsDistributedRateLimiterUnavailable},
}

func classifyLiveStatusFallbackDeferredError(err error) (livestatus.DeferredReason, bool) {
	for _, rule := range liveStatusFallbackDeferredRules {
		if rule.match(err) {
			return rule.reason, true
		}
	}
	return "", false
}

func isLiveStatusFallbackContextDone(err error) bool {
	return stdErrors.Is(err, context.Canceled) || stdErrors.Is(err, context.DeadlineExceeded)
}

func isLiveStatusFallbackYouTubeCooldown(err error) bool {
	return stdErrors.Is(err, scraper.ErrTransientCooldown)
}
