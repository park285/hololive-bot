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
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type LivePoller struct {
	client             *scraper.Client
	liveStatusProvider LiveStatusProvider
	db                 pollerDB
	baselineMu         sync.Mutex
	baselinedChannels  map[string]struct{}
}

type LiveStatusProvider interface {
	GetChannelsLiveStatus(ctx context.Context, channelIDs []string) ([]*domain.Stream, error)
}

func NewLivePoller(scraperClient *scraper.Client, db any) *LivePoller {
	return NewLivePollerWithStatusProvider(nil, scraperClient, db)
}

func NewLivePollerWithStatusProvider(provider LiveStatusProvider, scraperClient *scraper.Client, db any) *LivePoller {
	return &LivePoller{
		client:             scraperClient,
		liveStatusProvider: provider,
		db:                 normalizePollerDB(db),
		baselinedChannels:  make(map[string]struct{}),
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

	now := time.Now()
	baselinePoll := p.isBaselinePoll(channelID)

	for _, stream := range streams {
		if err := p.pollStream(ctx, channelID, stream, now, baselinePoll); err != nil {
			return err
		}
	}

	p.markEndedSessions(ctx, channelID, streams)
	p.markBaselineComplete(channelID)

	return nil
}

func (p *LivePoller) isBaselinePoll(channelID string) bool {
	p.baselineMu.Lock()
	defer p.baselineMu.Unlock()

	_, exists := p.baselinedChannels[channelID]
	return !exists
}

func (p *LivePoller) markBaselineComplete(channelID string) {
	p.baselineMu.Lock()
	defer p.baselineMu.Unlock()

	p.baselinedChannels[channelID] = struct{}{}
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

func (p *LivePoller) pollStream(ctx context.Context, channelID string, stream *domain.Stream, now time.Time, baselinePoll bool) error {
	status, ok := liveStatusFromStream(stream)
	if !ok {
		return nil
	}

	if err := p.saveLiveSession(ctx, channelID, stream, status, now, baselinePoll); err != nil {
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
	default:
		return "", false
	}
}
