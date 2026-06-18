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

package pollers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type LivePoller struct {
	client             *scraper.Client
	liveStatusProvider LiveStatusProvider
	db                 pollerDB
}

type LiveStatusProvider interface {
	GetChannelsLiveStatus(ctx context.Context, channelIDs []string) ([]*domain.Stream, error)
}

// failed map을 주는 provider만 fetch 실패 채널과 "방송 없음" 채널을 구분할 수 있다.
type LiveStatusWithFailuresProvider interface {
	GetChannelsLiveStatusWithFailures(ctx context.Context, channelIDs []string) ([]*domain.Stream, map[string]error, error)
}

func NewLivePoller(scraperClient *scraper.Client, db any) *LivePoller {
	return NewLivePollerWithStatusProvider(nil, scraperClient, db)
}

func NewLivePollerWithStatusProvider(provider LiveStatusProvider, scraperClient *scraper.Client, db any) *LivePoller {
	return &LivePoller{
		client:             scraperClient,
		liveStatusProvider: provider,
		db:                 normalizePollerDB(db),
	}
}

func (p *LivePoller) Name() string {
	return "live"
}

func (p *LivePoller) SetProxyEnabled(enabled bool) bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.SetProxyEnabled(enabled)
}

func (p *LivePoller) ProxyEnabled() bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.ProxyEnabled()
}

func (p *LivePoller) Poll(ctx context.Context, channelID string) error {
	streams, err := p.fetchLiveStreams(ctx, channelID)
	if err != nil {
		return fmt.Errorf("failed to get live streams: %w", err)
	}
	return p.pollLiveStreams(ctx, channelID, streams, time.Now())
}

func (p *LivePoller) PollBatch(ctx context.Context, channelIDs []string) map[string]error {
	ids := uniqueLiveChannelIDs(channelIDs)
	if len(ids) == 0 {
		return nil
	}
	if p.liveStatusProvider == nil {
		return p.pollBatchViaSinglePoll(ctx, ids)
	}

	streams, failures, err := p.fetchChannelsLiveStatusBatch(ctx, ids)
	if err != nil {
		return liveBatchErrorForAll(ids, fmt.Errorf("failed to get live streams batch: %w", err))
	}

	return p.pollGroupedLiveStreams(ctx, ids, groupLiveStreamsByChannel(ids, streams), failures)
}

func (p *LivePoller) fetchChannelsLiveStatusBatch(ctx context.Context, ids []string) ([]*domain.Stream, map[string]error, error) {
	if detailed, ok := p.liveStatusProvider.(LiveStatusWithFailuresProvider); ok {
		return detailed.GetChannelsLiveStatusWithFailures(ctx, ids)
	}
	streams, err := p.liveStatusProvider.GetChannelsLiveStatus(ctx, ids)
	return streams, nil, err
}

func (p *LivePoller) pollGroupedLiveStreams(
	ctx context.Context,
	ids []string,
	streamsByChannel map[string][]*domain.Stream,
	failures map[string]error,
) map[string]error {
	now := time.Now()
	errs := make(map[string]error)
	for _, channelID := range ids {
		if pollErr := p.pollBatchChannel(ctx, channelID, streamsByChannel[channelID], failures, now); pollErr != nil {
			errs[channelID] = pollErr
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func (p *LivePoller) pollBatchChannel(
	ctx context.Context,
	channelID string,
	streams []*domain.Stream,
	failures map[string]error,
	now time.Time,
) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if fetchErr, ok := failures[channelID]; ok {
		return fmt.Errorf("failed to get live streams: %w", fetchErr)
	}
	return p.pollLiveStreams(ctx, channelID, streams, now)
}

func (p *LivePoller) pollBatchViaSinglePoll(ctx context.Context, channelIDs []string) map[string]error {
	errs := make(map[string]error)
	for _, channelID := range channelIDs {
		if err := p.Poll(ctx, channelID); err != nil {
			errs[channelID] = err
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func (p *LivePoller) pollLiveStreams(ctx context.Context, channelID string, streams []*domain.Stream, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	for _, stream := range streams {
		if err := p.pollStream(ctx, channelID, stream, now); err != nil {
			return err
		}
	}
	p.markEndedSessions(ctx, channelID, streams)
	return nil
}

func uniqueLiveChannelIDs(channelIDs []string) []string {
	seen := make(map[string]struct{}, len(channelIDs))
	ids := make([]string, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		trimmed := strings.TrimSpace(channelID)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		ids = append(ids, trimmed)
	}
	return ids
}

func groupLiveStreamsByChannel(channelIDs []string, streams []*domain.Stream) map[string][]*domain.Stream {
	grouped := make(map[string][]*domain.Stream, len(channelIDs))
	for _, channelID := range channelIDs {
		grouped[channelID] = nil
	}

	for _, stream := range streams {
		channelID := liveStreamGroupKey(stream, channelIDs)
		if _, ok := grouped[channelID]; !ok {
			continue
		}
		grouped[channelID] = append(grouped[channelID], stream)
	}
	return grouped
}

func liveStreamGroupKey(stream *domain.Stream, channelIDs []string) string {
	if stream == nil {
		return ""
	}
	channelID := strings.TrimSpace(stream.ChannelID)
	if channelID == "" && len(channelIDs) == 1 {
		return channelIDs[0]
	}
	return channelID
}

func liveBatchErrorForAll(channelIDs []string, err error) map[string]error {
	if err == nil {
		return nil
	}
	errs := make(map[string]error, len(channelIDs))
	for _, channelID := range channelIDs {
		errs[channelID] = err
	}
	return errs
}

func (p *LivePoller) fetchLiveStreams(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	if p.liveStatusProvider != nil {
		return p.liveStatusProvider.GetChannelsLiveStatus(ctx, []string{channelID})
	}
	if p.client == nil {
		return nil, errors.New("live poller has no status provider or scraper client")
	}

	events, err := p.client.GetUpcomingEvents(ctx, channelID)
	if err != nil {
		return nil, err
	}
	return streamsFromUpcomingEvents(channelID, events), nil
}

func (p *LivePoller) pollStream(ctx context.Context, channelID string, stream *domain.Stream, now time.Time) error {
	status, ok := liveStatusFromStream(stream)
	if !ok {
		return nil
	}

	if err := p.saveLiveSession(ctx, channelID, stream, status, now); err != nil {
		return fmt.Errorf("poll live stream %s: %w", stream.ID, err)
	}

	if status == domain.LiveStatusLive {
		p.saveLiveViewerSample(ctx, channelID, stream, now)
	}

	return nil
}

func liveStatusFromStream(stream *domain.Stream) (domain.LiveStatus, bool) {
	if stream == nil {
		return "", false
	}
	switch stream.Status {
	case domain.StreamStatusLive:
		return domain.LiveStatusLive, true
	case domain.StreamStatusUpcoming:
		return domain.LiveStatusUpcoming, true
	case domain.StreamStatusPast:
		return "", false
	default:
		return "", false
	}
}
