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

package polling

import (
	"context"
	"fmt"
	"reflect"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/pollers"
)

type youTubeProducerPollerSet struct {
	videos           scheduler.Poller
	shorts           scheduler.Poller
	stats            scheduler.Poller
	live             scheduler.Poller
	liveBatch        *pollers.LivePoller
	liveBatchEnabled bool
}

type namedBackfillPoller struct {
	name string
	base scheduler.Poller
}

type unavailableYouTubeProducerPoller struct {
	name string
}

func newNamedBackfillPoller(name string, base scheduler.Poller) scheduler.Poller {
	return namedBackfillPoller{name: name, base: base}
}

func (p namedBackfillPoller) Poll(ctx context.Context, channelID string) error {
	return p.base.Poll(ctx, channelID)
}

func (p namedBackfillPoller) Name() string {
	return p.name
}

func (p namedBackfillPoller) SetProxyEnabled(enabled bool) bool {
	proxyPoller, ok := p.base.(interface {
		SetProxyEnabled(bool) bool
	})
	if !ok {
		return false
	}
	return proxyPoller.SetProxyEnabled(enabled)
}

func (p namedBackfillPoller) ProxyEnabled() bool {
	proxyPoller, ok := p.base.(interface {
		ProxyEnabled() bool
	})
	return ok && proxyPoller.ProxyEnabled()
}

func newUnavailableYouTubeProducerPoller(name string) scheduler.Poller {
	return unavailableYouTubeProducerPoller{name: name}
}

func (p unavailableYouTubeProducerPoller) Poll(context.Context, string) error {
	return fmt.Errorf("%s poller unavailable: database pool is nil", p.name)
}

func (p unavailableYouTubeProducerPoller) Name() string {
	return p.name
}

func CommunityKeywords() []string {
	return nil
}

func newYouTubeProducerPollerSet(
	scraperClient *scraper.Client,
	liveStatusProvider pollers.LiveStatusProvider,
	db any,
) youTubeProducerPollerSet {
	livePoller := pollers.NewLivePollerWithStatusProvider(liveStatusProvider, scraperClient, db)
	if !hasYouTubeProducerPollerDB(db) {
		return youTubeProducerPollerSet{
			videos:           newUnavailableYouTubeProducerPoller("videos"),
			shorts:           newUnavailableYouTubeProducerPoller("shorts"),
			stats:            newUnavailableYouTubeProducerPoller("channel_stats"),
			live:             livePoller,
			liveBatch:        livePoller,
			liveBatchEnabled: liveStatusProvider != nil,
		}
	}
	return youTubeProducerPollerSet{
		videos:           pollers.NewVideosPoller(scraperClient, db, defaultChannelPollerMaxResults),
		shorts:           pollers.NewShortsPoller(scraperClient, db, defaultChannelPollerMaxResults),
		stats:            pollers.NewChannelStatsPoller(scraperClient, db),
		live:             livePoller,
		liveBatch:        livePoller,
		liveBatchEnabled: liveStatusProvider != nil,
	}
}

func hasYouTubeProducerPollerDB(db any) bool {
	if db == nil {
		return false
	}
	value := reflect.ValueOf(db)
	kind := value.Kind()
	if kind == reflect.Chan ||
		kind == reflect.Func ||
		kind == reflect.Interface ||
		kind == reflect.Map ||
		kind == reflect.Pointer ||
		kind == reflect.Slice {
		return !value.IsNil()
	}
	return true
}
