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
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/internal/service/youtube/logschema"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

type premiereProbeResult struct {
	isPremiere     bool
	startTimestamp *time.Time
}

type watchLiveMetadataClient interface {
	GetWatchLiveMetadata(ctx context.Context, channelID, videoID string) (parser.WatchLiveMetadata, error)
}

func (p *LivePoller) probePremiere(ctx context.Context, channelID string, stream *domain.Stream) *bool {
	if p == nil || stream == nil {
		return nil
	}
	if p.watchLiveMetadataClient == nil || p.db == nil || stream.ID == "" {
		return premiereFromStream(stream)
	}
	result, found, started := p.beginPremiereProbe(stream.ID)
	if !started {
		return cachedPremiereProbeDecision(stream, result, found)
	}
	completed := false
	defer func() {
		p.finishPremiereProbe(stream.ID, result, completed)
	}()

	result, completed = p.resolvePremiereProbe(ctx, channelID, stream.ID)
	if !completed {
		return nil
	}
	applyPremiereProbeResult(stream, result)
	return &result.isPremiere
}

func cachedPremiereProbeDecision(stream *domain.Stream, result premiereProbeResult, found bool) *bool {
	if !found {
		return nil
	}
	applyPremiereProbeResult(stream, result)
	return &result.isPremiere
}

func (p *LivePoller) resolvePremiereProbe(ctx context.Context, channelID, videoID string) (premiereProbeResult, bool) {
	existing, found, err := loadExistingLiveSession(ctx, p.db, videoID)
	if err != nil {
		slog.WarnContext(ctx, "Premiere probe session lookup failed; decision stays pending",
			logschema.FieldChannelID, channelID,
			"video_id", videoID,
			"error", err,
		)
		return premiereProbeResult{}, false
	}
	if found && existing.IsPremiere != nil {
		return premiereProbeResult{isPremiere: *existing.IsPremiere}, true
	}
	return p.fetchPremiereProbe(ctx, channelID, videoID)
}

func (p *LivePoller) fetchPremiereProbe(ctx context.Context, channelID, videoID string) (premiereProbeResult, bool) {
	metadata, err := p.watchLiveMetadataClient.GetWatchLiveMetadata(ctx, channelID, videoID)
	if err != nil {
		slog.WarnContext(ctx, "Premiere watch probe failed; decision stays pending",
			logschema.FieldChannelID, channelID,
			"video_id", videoID,
			"error", err,
		)
		return premiereProbeResult{}, false
	}
	isPremiere, decided := premiereDecisionFromLiveContent(metadata.LiveContent)
	if !decided {
		slog.WarnContext(ctx, "Premiere watch probe returned unknown live-content; decision stays pending",
			logschema.FieldChannelID, channelID,
			"video_id", videoID,
		)
		return premiereProbeResult{}, false
	}
	result := premiereProbeResult{isPremiere: isPremiere}
	if metadata.StartTimestamp != nil {
		startTimestamp := metadata.StartTimestamp.UTC()
		result.startTimestamp = &startTimestamp
	}
	return result, true
}

func premiereDecisionFromLiveContent(liveContent parser.LiveContentStatus) (isPremiere, decided bool) {
	switch liveContent {
	case parser.LiveContentTrue:
		return false, true
	case parser.LiveContentFalse:
		return true, true
	case parser.LiveContentUnknown:
		return false, false
	default:
		return false, false
	}
}

func applyPremiereProbeResult(stream *domain.Stream, result premiereProbeResult) {
	stream.IsPremiere = result.isPremiere
	if stream.StartScheduled == nil && result.startTimestamp != nil {
		startTimestamp := result.startTimestamp.UTC()
		stream.StartScheduled = &startTimestamp
	}
}

func (p *LivePoller) beginPremiereProbe(videoID string) (premiereProbeResult, bool, bool) {
	p.premiereProbeMu.Lock()
	defer p.premiereProbeMu.Unlock()
	if result, ok := p.premiereProbeCompleted[videoID]; ok {
		return result, true, false
	}
	if _, ok := p.premiereProbeInProgress[videoID]; ok {
		return premiereProbeResult{}, false, false
	}
	p.premiereProbeInProgress[videoID] = struct{}{}
	return premiereProbeResult{}, false, true
}

func (p *LivePoller) finishPremiereProbe(videoID string, result premiereProbeResult, completed bool) {
	p.premiereProbeMu.Lock()
	defer p.premiereProbeMu.Unlock()
	delete(p.premiereProbeInProgress, videoID)
	if completed {
		p.premiereProbeCompleted[videoID] = result
	}
}

func (p *LivePoller) forgetPremiereProbe(videoID string) {
	p.premiereProbeMu.Lock()
	defer p.premiereProbeMu.Unlock()
	delete(p.premiereProbeCompleted, videoID)
}
