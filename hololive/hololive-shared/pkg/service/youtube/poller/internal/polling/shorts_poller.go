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
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/polling/batchrepo"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

type ShortsPoller struct {
	client                           *scraper.Client
	db                               *gorm.DB
	repository                       batchrepo.BatchRepository
	maxResults                       int
	routeDecider                     NotificationRouteDecider
	inlinePublishedAtFallbackEnabled bool
}

func NewShortsPoller(scraperClient *scraper.Client, db *gorm.DB, maxResults int, routeDecider NotificationRouteDecider, inlinePublishedAtFallbackEnabled ...bool) *ShortsPoller {
	if maxResults <= 0 {
		maxResults = 10
	}
	inlineFallbackEnabled := false
	if len(inlinePublishedAtFallbackEnabled) > 0 {
		inlineFallbackEnabled = inlinePublishedAtFallbackEnabled[0]
	}
	return &ShortsPoller{
		client:                           scraperClient,
		db:                               db,
		repository:                       batchrepo.NewBatchRepository(db),
		maxResults:                       maxResults,
		routeDecider:                     routeDecider,
		inlinePublishedAtFallbackEnabled: inlineFallbackEnabled,
	}
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

	shorts = normalizeCollectedShortsByCanonicalPostID(shorts)
	if len(shorts) == 0 {
		return nil
	}

	watermark, isInitialized, err := loadContentWatermark(ctx, p.db, channelID, domain.WatermarkTypeShort)
	if err != nil {
		return err
	}
	newShorts := collectNewShorts(shorts, watermark, isInitialized)
	if isInitialized && len(newShorts) > 0 && p.inlinePublishedAtFallbackEnabled && shortsNeedPublishedAtLookup(newShorts) {
		p.client.EnrichShortsPublishedAtFromRSS(ctx, channelID, newShorts)
	}

	detectedAt := yttimestamp.Normalize(time.Now())
	observeCommunityShortsDetectionBatch(ctx, channelID, domain.AlarmTypeShorts, len(newShorts), detectedAt)
	batch := p.buildShortBatch(ctx, channelID, newShorts, isInitialized, detectedAt)

	if err := p.repository.PersistVideos(ctx, batch.dbVideos, batch.notifications, batch.trackingRows, &domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: selectLastWatermarkContentID(domain.OutboxKindNewShort, shorts[0].VideoID, watermark.LastContentID, batch.keepExistingWatermark),
	}); err != nil {
		return fmt.Errorf("persist short batch: %w", err)
	}

	return nil
}

type shortsPollBatch struct {
	dbVideos              []*domain.YouTubeVideo
	notifications         []*domain.YouTubeNotificationOutbox
	trackingRows          []*domain.YouTubeContentAlarmTracking
	keepExistingWatermark bool
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
		dbVideo, trackingRow, notification, keepExistingWatermark := p.buildShortArtifacts(ctx, channelID, shorts[i], isInitialized, detectedAt)
		if dbVideo != nil {
			batch.dbVideos = append(batch.dbVideos, dbVideo)
		}
		if trackingRow != nil {
			batch.trackingRows = append(batch.trackingRows, trackingRow)
		}
		if notification != nil {
			batch.notifications = append(batch.notifications, notification)
		}
		batch.keepExistingWatermark = batch.keepExistingWatermark || keepExistingWatermark
	}
	return batch
}

func (p *ShortsPoller) buildShortArtifacts(
	ctx context.Context,
	channelID string,
	short *scraper.Short,
	isInitialized bool,
	detectedAt time.Time,
) (*domain.YouTubeVideo, *domain.YouTubeContentAlarmTracking, *domain.YouTubeNotificationOutbox, bool) {
	if short == nil {
		return nil, nil, nil, false
	}

	canonicalPostID := normalizeContentID(domain.OutboxKindNewShort, short.VideoID)
	resourceVideoID := normalizeShortVideoResourceID(short.VideoID)
	publishedAt := yttimestamp.NormalizePtr(short.PublishedAt)
	if isInitialized && publishedAt == nil && p.inlinePublishedAtFallbackEnabled {
		publishedAt = p.resolveShortPublishedAtInline(ctx, resourceVideoID)
	}
	dbVideo := &domain.YouTubeVideo{
		VideoID:     resourceVideoID,
		ChannelID:   channelID,
		Title:       short.Title,
		Thumbnail:   convertThumbnails(short.Thumbnail),
		PublishedAt: publishedAt,
		IsShort:     true,
		ViewCount:   short.ViewCount,
	}
	logShortDetected(ctx, channelID, canonicalPostID, dbVideo.PublishedAt, detectedAt)
	if !isInitialized {
		return dbVideo, nil, nil, false
	}

	trackingRow := &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindNewShort,
		ContentID:         canonicalPostID,
		ChannelID:         channelID,
		ActualPublishedAt: dbVideo.PublishedAt,
		DetectedAt:        detectedAt,
	}
	notification, keepExistingWatermark := p.buildShortNotification(channelID, canonicalPostID, dbVideo)
	return dbVideo, trackingRow, notification, keepExistingWatermark
}

func (p *ShortsPoller) buildShortNotification(
	channelID string,
	canonicalPostID string,
	dbVideo *domain.YouTubeVideo,
) (*domain.YouTubeNotificationOutbox, bool) {
	routePublishedAt := derefTime(dbVideo.PublishedAt)
	if p.routeDecider != nil && routePublishedAt.IsZero() {
		return nil, p.inlinePublishedAtFallbackEnabled
	}
	if p.routeDecider != nil && !shouldEnqueueRoutedNotification(p.routeDecider, domain.AlarmTypeShorts, channelID, routePublishedAt) {
		return nil, false
	}

	return &domain.YouTubeNotificationOutbox{
		Kind:      domain.OutboxKindNewShort,
		ChannelID: channelID,
		ContentID: canonicalPostID,
		Payload:   buildShortNotificationPayload(dbVideo, canonicalPostID),
		Status:    domain.OutboxStatusPending,
	}, false
}

func collectNewShorts(
	shorts []*scraper.Short,
	watermark domain.YouTubeContentWatermark,
	isInitialized bool,
) []*scraper.Short {
	newShorts := make([]*scraper.Short, 0, len(shorts))
	for _, short := range shorts {
		canonicalPostID := normalizeContentID(domain.OutboxKindNewShort, short.VideoID)
		if isInitialized && canonicalPostID == normalizeContentID(domain.OutboxKindNewShort, watermark.LastContentID) {
			break
		}
		newShorts = append(newShorts, short)
	}
	return newShorts
}

func shortsNeedPublishedAtLookup(shorts []*scraper.Short) bool {
	for _, short := range shorts {
		if short != nil && short.PublishedAt == nil {
			return true
		}
	}
	return false
}

func (p *ShortsPoller) resolveShortPublishedAtInline(ctx context.Context, videoID string) *time.Time {
	if strings.TrimSpace(videoID) == "" {
		return nil
	}

	resolveCtx, cancel := context.WithTimeout(ctx, inlinePublishedAtFallbackTimeout)
	defer cancel()

	publishedAt, err := p.client.ResolveVideoPublishedAt(resolveCtx, videoID)
	if err != nil {
		if errors.Is(err, scraper.ErrPublishedAtNotFound) {
			return nil
		}
		slog.WarnContext(ctx, "short published_at inline fallback failed",
			"video_id", videoID,
			"error", err,
		)
		return nil
	}

	return yttimestamp.NormalizePtr(publishedAt)
}

func logShortDetected(ctx context.Context, channelID, postID string, actualPublishedAt *time.Time, detectedAt time.Time) {
	slog.LogAttrs(ctx, slog.LevelInfo, shortDetectedLogMessage,
		slog.String(logschema.FieldChannelID, channelID),
		slog.String(logschema.FieldPostID, postID),
		optionalTimestampAttr(logschema.FieldActualPublishedAt, actualPublishedAt),
		slog.String(logschema.FieldDetectedAt, yttimestamp.Format(detectedAt)),
	)
}
