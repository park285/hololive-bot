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
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/community"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

type CommunityPoller struct {
	client     *scraper.Client
	db         pollerDB
	repository batchrepo.BatchRepository
	maxResults int
	keywords   []string
	metrics    *polling.Metrics
}

func NewCommunityPoller(scraperClient *scraper.Client, db any, maxResults int, keywords []string) *CommunityPoller {
	if maxResults <= 0 {
		maxResults = 10
	}
	querier := normalizePollerDB(db)
	return &CommunityPoller{
		client:     scraperClient,
		db:         querier,
		repository: batchrepo.NewPgxBatchRepositoryWithPersister(querier, newDeliveryTelemetryLatencyPersisterAdapter(querier)),
		maxResults: maxResults,
		keywords:   community.NormalizeKeywords(keywords),
	}
}

func (p *CommunityPoller) SetMetrics(m *polling.Metrics) {
	if p == nil {
		return
	}
	p.metrics = m
}

func (p *CommunityPoller) ensureMetrics() *polling.Metrics {
	if p.metrics != nil {
		return p.metrics
	}
	return polling.NewMetrics()
}

func (p *CommunityPoller) persistCollected(ctx context.Context, channelID string, posts []*parser.CommunityPost) error {
	watermark, isInitialized, err := loadContentWatermark(ctx, p.db, channelID, domain.WatermarkTypeCommunityPost)
	if err != nil {
		return err
	}
	newPosts := community.CollectNewPosts(posts, &watermark, isInitialized)
	detectedAt := yttimestamp.Normalize(time.Now()).Truncate(time.Microsecond)
	observeCommunityShortsDetectionBatch(ctx, channelID, domain.AlarmTypeCommunity, len(newPosts), detectedAt, p.ensureMetrics())
	batch := p.buildCommunityBatch(ctx, channelID, newPosts, isInitialized, detectedAt)

	if err := p.repository.PersistCommunityPosts(ctx, batch.dbPosts, batch.notifications, batch.trackingRows, &domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeCommunityPost,
		Initialized:   true,
		LastContentID: polling.NormalizeContentID(domain.OutboxKindCommunityPost, posts[0].PostID),
	}); err != nil {
		return fmt.Errorf("persist community batch: %w", err)
	}

	return nil
}

type communityPollBatch struct {
	dbPosts       []*domain.YouTubeCommunityPost
	notifications []*domain.YouTubeNotificationOutbox
	trackingRows  []*domain.YouTubeContentAlarmTracking
}

func (p *CommunityPoller) buildCommunityBatch(
	ctx context.Context,
	channelID string,
	posts []*parser.CommunityPost,
	isInitialized bool,
	detectedAt time.Time,
) communityPollBatch {
	batch := communityPollBatch{
		dbPosts:       make([]*domain.YouTubeCommunityPost, 0, len(posts)),
		notifications: make([]*domain.YouTubeNotificationOutbox, 0, len(posts)),
		trackingRows:  make([]*domain.YouTubeContentAlarmTracking, 0, len(posts)),
	}
	for i := range posts {
		dbPost, trackingRow, notification := community.BuildPostArtifacts(channelID, posts[i], isInitialized, detectedAt, p.keywords)
		if dbPost != nil {
			logCommunityPostDetected(ctx, channelID, dbPost.PostID, dbPost.PublishedAt, detectedAt)
			batch.dbPosts = append(batch.dbPosts, dbPost)
		}
		if trackingRow != nil {
			batch.trackingRows = append(batch.trackingRows, trackingRow)
		}
		if notification != nil {
			batch.notifications = append(batch.notifications, notification)
		}
	}
	return batch
}

func logCommunityPostDetected(ctx context.Context, channelID, postID string, actualPublishedAt *time.Time, detectedAt time.Time) {
	slog.LogAttrs(ctx, slog.LevelInfo, communityPostDetectedLogMessage,
		slog.String(logschema.FieldChannelID, channelID),
		slog.String(logschema.FieldPostID, postID),
		optionalTimestampAttr(logschema.FieldActualPublishedAt, actualPublishedAt),
		slog.String(logschema.FieldDetectedAt, yttimestamp.Format(detectedAt)),
	)
}

func optionalTimestampAttr(key string, value *time.Time) slog.Attr {
	if value == nil {
		return slog.Any(key, nil)
	}
	return slog.String(key, yttimestamp.Format(*value))
}
