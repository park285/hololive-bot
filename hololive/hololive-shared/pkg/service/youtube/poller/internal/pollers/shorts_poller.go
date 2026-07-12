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

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/batchrepo"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

type ShortsPoller struct {
	client     *scraper.Client
	db         pollerDB
	repository batchrepo.BatchRepository
	maxResults int
	metrics    *polling.Metrics
	deferrals  *freshnessDeferrals
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
		deferrals:  newFreshnessDeferrals(),
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
		p.reconcileShortDeferrals(ctx, channelID, nil, &shortsPollBatch{deferredVideoIDs: make(map[string]struct{})}, nil)

		return nil
	}

	watermark, isInitialized, err := loadContentWatermark(ctx, p.db, channelID, domain.WatermarkTypeShort)
	if err != nil {
		return err
	}

	detectedAt := yttimestamp.Normalize(time.Now()).Truncate(time.Microsecond)
	nextWatermark := domain.YouTubeContentWatermark{
		ChannelID:     channelID,
		WatermarkType: domain.WatermarkTypeShort,
		Initialized:   true,
		LastContentID: polling.NormalizeContentID(domain.OutboxKindNewShort, shorts[0].VideoID),
	}

	if !isInitialized {
		return p.persistShortBaseline(ctx, channelID, shorts, detectedAt, &nextWatermark)
	}
	return p.pollInitializedShorts(ctx, channelID, shorts, &watermark, detectedAt, &nextWatermark)
}

func (p *ShortsPoller) persistShortBaseline(
	ctx context.Context,
	channelID string,
	shorts []*scraper.Short,
	detectedAt time.Time,
	nextWatermark *domain.YouTubeContentWatermark,
) error {
	batch := p.buildClassifiedShortBatch(ctx, channelID, baselineShortCandidates(shorts), detectedAt)
	observeCommunityShortsDetectionBatch(ctx, channelID, domain.AlarmTypeShorts, 0, detectedAt, p.ensureMetrics())
	if err := p.repository.PersistVideos(ctx, batch.dbVideos, batch.notifications, batch.trackingRows, nextWatermark); err != nil {
		return fmt.Errorf("persist short baseline batch: %w", err)
	}
	return nil
}

func (p *ShortsPoller) pollInitializedShorts(
	ctx context.Context,
	channelID string,
	shorts []*scraper.Short,
	watermark *domain.YouTubeContentWatermark,
	detectedAt time.Time,
	nextWatermark *domain.YouTubeContentWatermark,
) error {
	positionalNew, watermarkFound := collectNewShorts(shorts, watermark, true)
	if !watermarkFound {
		slog.WarnContext(ctx, "Shorts watermark missing from collected page; classifying full page by publication freshness",
			logschema.FieldChannelID, channelID,
			"collected_count", len(shorts),
		)
	}

	scrapedIDs := shortRawResourceIDs(shorts)
	scrapedIDSet := shortRawResourceIDSet(shorts)
	states, err := loadShortVideoStates(ctx, p.db, scrapedIDs)
	if err != nil {
		return err
	}

	candidateIDs := shortRawResourceIDSet(positionalNew)
	for videoID := range p.deferrals.trackedIDs(channelID) {
		if _, scraped := scrapedIDSet[videoID]; scraped {
			candidateIDs[videoID] = struct{}{}
		}
	}
	classified := p.classifyShortCandidates(ctx, channelID, shorts, candidateIDs, states, detectedAt)
	batch := p.buildClassifiedShortBatch(ctx, channelID, classified, detectedAt)
	persistWatermark := p.reconcileShortDeferrals(ctx, channelID, shorts, &batch, nextWatermark)
	observeCommunityShortsDetectionBatch(ctx, channelID, domain.AlarmTypeShorts, batch.detectedCount, detectedAt, p.ensureMetrics())

	if err := p.repository.PersistVideos(ctx, batch.dbVideos, batch.notifications, batch.trackingRows, persistWatermark); err != nil {
		return fmt.Errorf("persist short batch: %w", err)
	}
	return nil
}

func (p *ShortsPoller) reconcileShortDeferrals(
	ctx context.Context,
	channelID string,
	shorts []*scraper.Short,
	batch *shortsPollBatch,
	nextWatermark *domain.YouTubeContentWatermark,
) *domain.YouTubeContentWatermark {
	holdWatermark, departed := p.deferrals.reconcileChannel(channelID, shortRawResourceIDSet(shorts), batch.deferredVideoIDs)
	for _, videoID := range departed {
		slog.WarnContext(ctx, "Shorts deferred candidate left the collected page; dropping without notification after max attempts",
			logschema.FieldChannelID, channelID,
			"video_id", videoID,
		)
	}
	if !holdWatermark {
		return nextWatermark
	}
	slog.WarnContext(ctx, "Shorts watermark held while freshness candidates stay deferred",
		logschema.FieldChannelID, channelID,
		"deferred_count", len(batch.deferredVideoIDs),
	)
	return nil
}

type shortsPollBatch struct {
	dbVideos         []*domain.YouTubeVideo
	notifications    []*domain.YouTubeNotificationOutbox
	trackingRows     []*domain.YouTubeContentAlarmTracking
	deferredVideoIDs map[string]struct{}
	detectedCount    int
}

func baselineShortCandidates(shorts []*scraper.Short) []classifiedShortCandidate {
	candidates := make([]classifiedShortCandidate, 0, len(shorts))
	for _, short := range shorts {
		if short == nil {
			continue
		}
		candidates = append(candidates, classifiedShortCandidate{
			short:       short,
			class:       shortCandidateStoreSilently,
			publishedAt: yttimestamp.NormalizePtr(short.PublishedAt),
		})
	}
	return candidates
}

func (p *ShortsPoller) buildClassifiedShortBatch(
	ctx context.Context,
	channelID string,
	classified []classifiedShortCandidate,
	detectedAt time.Time,
) shortsPollBatch {
	batch := shortsPollBatch{
		dbVideos:         make([]*domain.YouTubeVideo, 0, len(classified)),
		notifications:    make([]*domain.YouTubeNotificationOutbox, 0, len(classified)),
		trackingRows:     make([]*domain.YouTubeContentAlarmTracking, 0, len(classified)),
		deferredVideoIDs: make(map[string]struct{}),
	}
	for _, candidate := range classified {
		p.appendShortCandidate(ctx, &batch, channelID, candidate, detectedAt)
	}
	return batch
}

func (p *ShortsPoller) appendShortCandidate(
	ctx context.Context,
	batch *shortsPollBatch,
	channelID string,
	candidate classifiedShortCandidate,
	detectedAt time.Time,
) {
	if candidate.short == nil {
		return
	}
	canonicalPostID := polling.NormalizeContentID(domain.OutboxKindNewShort, candidate.short.VideoID)
	if candidate.class == shortCandidateDeferred {
		batch.deferredVideoIDs[polling.NormalizeShortVideoResourceID(candidate.short.VideoID)] = struct{}{}
		return
	}

	dbVideo := buildShortVideoRecord(channelID, candidate.short, candidate.publishedAt)
	batch.dbVideos = append(batch.dbVideos, dbVideo)
	logShortDetected(ctx, channelID, canonicalPostID, dbVideo.PublishedAt, detectedAt)
	if candidate.class == shortCandidateStoreSilently {
		return
	}

	batch.trackingRows = append(batch.trackingRows, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindNewShort,
		ContentID:         canonicalPostID,
		ChannelID:         channelID,
		ActualPublishedAt: dbVideo.PublishedAt,
		DetectedAt:        detectedAt,
	})
	batch.notifications = append(batch.notifications, p.buildShortNotification(channelID, canonicalPostID, dbVideo))
	if candidate.class == shortCandidateNotifyFresh {
		batch.detectedCount++
	}
}

func buildShortVideoRecord(channelID string, short *scraper.Short, publishedAt *time.Time) *domain.YouTubeVideo {
	return &domain.YouTubeVideo{
		VideoID:     polling.NormalizeShortVideoResourceID(short.VideoID),
		ChannelID:   channelID,
		Title:       short.Title,
		Thumbnail:   polling.ConvertThumbnails(short.Thumbnail),
		PublishedAt: yttimestamp.NormalizePtr(publishedAt),
		IsShort:     true,
		ViewCount:   short.ViewCount,
	}
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
) ([]*scraper.Short, bool) {
	newShorts := make([]*scraper.Short, 0, len(shorts))
	for _, short := range shorts {
		canonicalPostID := polling.NormalizeContentID(domain.OutboxKindNewShort, short.VideoID)
		if isInitialized && canonicalPostID == polling.NormalizeContentID(domain.OutboxKindNewShort, watermark.LastContentID) {
			return newShorts, true
		}
		newShorts = append(newShorts, short)
	}
	return newShorts, false
}

func logShortDetected(ctx context.Context, channelID, postID string, actualPublishedAt *time.Time, detectedAt time.Time) {
	slog.LogAttrs(ctx, slog.LevelInfo, shortDetectedLogMessage,
		slog.String(logschema.FieldChannelID, channelID),
		slog.String(logschema.FieldPostID, postID),
		optionalTimestampAttr(logschema.FieldActualPublishedAt, actualPublishedAt),
		slog.String(logschema.FieldDetectedAt, yttimestamp.Format(detectedAt)),
	)
}
