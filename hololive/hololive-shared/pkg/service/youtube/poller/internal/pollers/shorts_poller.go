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

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/batchrepo"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

type ShortsPoller struct {
	client     *scraper.Client
	db         pollerDB
	repository batchrepo.BatchRepository
	maxResults int
	metrics    *polling.Metrics
}

func NewShortsPoller(scraperClient *scraper.Client, db any, maxResults int) *ShortsPoller {
	if maxResults <= 0 {
		maxResults = 10
	}
	querier := normalizePollerDB(db)
	return &ShortsPoller{
		client:     scraperClient,
		db:         querier,
		repository: batchrepo.NewPgxBatchRepositoryWithPersister(querier, newDeliveryTelemetryLatencyPersisterAdapter(querier)),
		maxResults: maxResults,
	}
}

func (p *ShortsPoller) SetMetrics(m *polling.Metrics) {
	if p == nil {
		return
	}
	p.metrics = m
}

func (p *ShortsPoller) ensureMetrics() *polling.Metrics {
	if p.metrics != nil {
		return p.metrics
	}
	return polling.NewMetrics()
}

func (p *ShortsPoller) Name() string {
	return "shorts"
}

func (p *ShortsPoller) SetProxyEnabled(enabled bool) bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.SetProxyEnabled(enabled)
}

func (p *ShortsPoller) ProxyEnabled() bool {
	if p == nil || p.client == nil {
		return false
	}
	return p.client.ProxyEnabled()
}

func (p *ShortsPoller) Poll(ctx context.Context, channelID string) error {
	shorts, err := p.client.GetShorts(ctx, channelID, p.maxResults)
	if err != nil {
		return fmt.Errorf("failed to get shorts: %w", err)
	}

	shorts = polling.NormalizeCollectedShortsByCanonicalPostID(shorts)
	if len(shorts) == 0 {
		return nil
	}

	watermark, isInitialized, err := loadContentWatermark(ctx, p.db, channelID, domain.WatermarkTypeShort)
	if err != nil {
		return err
	}
	newShorts := collectNewShorts(shorts, &watermark, isInitialized)

	detectedAt := yttimestamp.Normalize(time.Now()).Truncate(time.Microsecond)
	observeCommunityShortsDetectionBatch(ctx, channelID, domain.AlarmTypeShorts, len(newShorts), detectedAt, p.ensureMetrics())
	batch := p.buildShortBatch(ctx, channelID, newShorts, isInitialized, detectedAt)

	if err := p.repository.PersistVideos(ctx, batch.dbVideos, batch.notifications, batch.trackingRows, &domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: polling.NormalizeContentID(domain.OutboxKindNewShort, shorts[0].VideoID),
	}); err != nil {
		return fmt.Errorf("persist short batch: %w", err)
	}

	return nil
}

type shortsPollBatch struct {
	dbVideos      []*domain.YouTubeVideo
	notifications []*domain.YouTubeNotificationOutbox
	trackingRows  []*domain.YouTubeContentAlarmTracking
}

func (p *ShortsPoller) buildShortBatch(
	ctx context.Context,
	channelID string,
	shorts []*scraper.Short,
	isInitialized bool,
	detectedAt time.Time,
) shortsPollBatch {
	batch := shortsPollBatch{
		dbVideos:      make([]*domain.YouTubeVideo, 0, len(shorts)),
		notifications: make([]*domain.YouTubeNotificationOutbox, 0, len(shorts)),
		trackingRows:  make([]*domain.YouTubeContentAlarmTracking, 0, len(shorts)),
	}
	for i := range shorts {
		dbVideo, trackingRow, notification := p.buildShortArtifacts(ctx, channelID, shorts[i], isInitialized, detectedAt)
		if dbVideo != nil {
			batch.dbVideos = append(batch.dbVideos, dbVideo)
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

func (p *ShortsPoller) buildShortArtifacts(
	ctx context.Context,
	channelID string,
	short *scraper.Short,
	isInitialized bool,
	detectedAt time.Time,
) (*domain.YouTubeVideo, *domain.YouTubeContentAlarmTracking, *domain.YouTubeNotificationOutbox) {
	if short == nil {
		return nil, nil, nil
	}

	canonicalPostID := polling.NormalizeContentID(domain.OutboxKindNewShort, short.VideoID)
	resourceVideoID := polling.NormalizeShortVideoResourceID(short.VideoID)
	publishedAt := yttimestamp.NormalizePtr(short.PublishedAt)
	dbVideo := &domain.YouTubeVideo{
		VideoID:     resourceVideoID,
		ChannelID:   channelID,
		Title:       short.Title,
		Thumbnail:   polling.ConvertThumbnails(short.Thumbnail),
		PublishedAt: publishedAt,
		IsShort:     true,
		ViewCount:   short.ViewCount,
	}
	logShortDetected(ctx, channelID, canonicalPostID, dbVideo.PublishedAt, detectedAt)
	if !isInitialized {
		return dbVideo, nil, nil
	}

	trackingRow := &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindNewShort,
		ContentID:         canonicalPostID,
		ChannelID:         channelID,
		ActualPublishedAt: dbVideo.PublishedAt,
		DetectedAt:        detectedAt,
	}
	notification := p.buildShortNotification(channelID, canonicalPostID, dbVideo)
	return dbVideo, trackingRow, notification
}

func (p *ShortsPoller) buildShortNotification(
	channelID string,
	canonicalPostID string,
	dbVideo *domain.YouTubeVideo,
) *domain.YouTubeNotificationOutbox {
	return &domain.YouTubeNotificationOutbox{
		Kind:      domain.OutboxKindNewShort,
		ChannelID: channelID,
		ContentID: canonicalPostID,
		Payload:   polling.BuildShortNotificationPayload(dbVideo, canonicalPostID),
		Status:    domain.OutboxStatusPending,
	}
}

func collectNewShorts(
	shorts []*scraper.Short,
	watermark *domain.YouTubeContentWatermark,
	isInitialized bool,
) []*scraper.Short {
	newShorts := make([]*scraper.Short, 0, len(shorts))
	for _, short := range shorts {
		canonicalPostID := polling.NormalizeContentID(domain.OutboxKindNewShort, short.VideoID)
		if isInitialized && canonicalPostID == polling.NormalizeContentID(domain.OutboxKindNewShort, watermark.LastContentID) {
			break
		}
		newShorts = append(newShorts, short)
	}
	return newShorts
}

func logShortDetected(ctx context.Context, channelID, postID string, actualPublishedAt *time.Time, detectedAt time.Time) {
	slog.LogAttrs(ctx, slog.LevelInfo, shortDetectedLogMessage,
		slog.String(logschema.FieldChannelID, channelID),
		slog.String(logschema.FieldPostID, postID),
		optionalTimestampAttr(logschema.FieldActualPublishedAt, actualPublishedAt),
		slog.String(logschema.FieldDetectedAt, yttimestamp.Format(detectedAt)),
	)
}
