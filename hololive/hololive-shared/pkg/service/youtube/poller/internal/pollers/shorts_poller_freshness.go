package pollers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

type shortVideoStateRow struct {
	VideoID     string     `db:"video_id"`
	IsShort     bool       `db:"is_short"`
	PublishedAt *time.Time `db:"published_at"`
	FirstSeenAt time.Time  `db:"first_seen_at"`
}

func loadShortVideoStates(ctx context.Context, db dbx.Querier, videoIDs []string) (map[string]shortVideoStateRow, error) {
	states := make(map[string]shortVideoStateRow, len(videoIDs))
	if len(videoIDs) == 0 {
		return states, nil
	}

	var rows []shortVideoStateRow
	query := mustSQL("shorts_poller_freshness_0038_01.sql") + dbx.InPlaceholders(len(videoIDs)) + `)`
	if err := dbx.SelectSQL(ctx, db, &rows, "query short video freshness states", query, dbx.AnyArgs(videoIDs)...); err != nil {
		return nil, fmt.Errorf("query short video freshness states: %w", err)
	}
	for i := range rows {
		states[rows[i].VideoID] = rows[i]
	}
	return states, nil
}

type shortCandidateClass int

const (
	shortCandidateNotifyFresh shortCandidateClass = iota
	shortCandidateStoreSilently
	shortCandidateReoffer
	shortCandidateDeferred
)

type classifiedShortCandidate struct {
	short       *scraper.Short
	class       shortCandidateClass
	publishedAt *time.Time
}

func (p *ShortsPoller) classifyShortCandidates(
	ctx context.Context,
	channelID string,
	shorts []*scraper.Short,
	candidateIDs map[string]struct{},
	states map[string]shortVideoStateRow,
	now time.Time,
) []classifiedShortCandidate {
	classified := make([]classifiedShortCandidate, 0, len(shorts))
	for _, short := range shorts {
		if short == nil {
			continue
		}
		rawID := polling.NormalizeShortVideoResourceID(short.VideoID)
		if _, isCandidate := candidateIDs[rawID]; !isCandidate {
			continue
		}
		state, known := states[rawID]
		if known && state.IsShort {
			classified = append(classified, classifiedShortCandidate{
				short:       short,
				class:       shortCandidateReoffer,
				publishedAt: yttimestamp.NormalizePtr(state.PublishedAt),
			})
			continue
		}
		classified = append(classified, p.classifyShortByFreshness(ctx, channelID, short, rawID, state, known, now))
	}
	return classified
}

func (p *ShortsPoller) classifyShortByFreshness(
	ctx context.Context,
	channelID string,
	short *scraper.Short,
	rawID string,
	state shortVideoStateRow,
	known bool,
	now time.Time,
) classifiedShortCandidate {
	publishedAt, historical := shortPublishedAtEvidence(short, state, known, now)
	if historical {
		p.deferrals.clear(channelID, rawID)
		return classifiedShortCandidate{short: short, class: shortCandidateStoreSilently}
	}
	if publishedAt == nil {
		publishedAt = p.resolveShortPublishedAt(ctx, channelID, short.VideoID)
	}
	return p.classifyShortTimestamp(ctx, channelID, short, rawID, publishedAt, now)
}

func shortPublishedAtEvidence(
	short *scraper.Short,
	state shortVideoStateRow,
	known bool,
	now time.Time,
) (*time.Time, bool) {
	if publishedAt := yttimestamp.NormalizePtr(short.PublishedAt); publishedAt != nil {
		return publishedAt, false
	}
	if !known {
		return nil, false
	}
	publishedAt := yttimestamp.NormalizePtr(state.PublishedAt)
	historical := publishedAt == nil && now.Sub(state.FirstSeenAt) > publicationFreshnessHorizon
	return publishedAt, historical
}

func (p *ShortsPoller) classifyShortTimestamp(
	ctx context.Context,
	channelID string,
	short *scraper.Short,
	rawID string,
	publishedAt *time.Time,
	now time.Time,
) classifiedShortCandidate {
	if publishedAt == nil {
		return p.deferOrAbsorbShortCandidate(ctx, channelID, short, rawID, "published_at unresolved")
	}

	age := now.Sub(*publishedAt)
	switch {
	case age > publicationFreshnessHorizon:
		p.deferrals.clear(channelID, rawID)
		return classifiedShortCandidate{short: short, class: shortCandidateStoreSilently, publishedAt: publishedAt}
	case age >= -publicationFreshnessFutureSkew:
		p.deferrals.clear(channelID, rawID)
		return classifiedShortCandidate{short: short, class: shortCandidateNotifyFresh, publishedAt: publishedAt}
	default:
		return p.deferOrAbsorbShortCandidate(ctx, channelID, short, rawID,
			fmt.Sprintf("published_at %s is beyond the future skew allowance", publishedAt.Format(time.RFC3339)))
	}
}

func (p *ShortsPoller) deferOrAbsorbShortCandidate(
	ctx context.Context,
	channelID string,
	short *scraper.Short,
	rawID string,
	reason string,
) classifiedShortCandidate {
	attempts := p.deferrals.recordFailure(channelID, rawID)
	if attempts < publicationFreshnessMaxAttempts {
		slog.WarnContext(ctx, "Shorts freshness unresolved; deferring candidate without notification",
			logschema.FieldChannelID, channelID,
			"video_id", rawID,
			"attempts", attempts,
			"reason", reason,
		)
		return classifiedShortCandidate{short: short, class: shortCandidateDeferred}
	}
	p.deferrals.clear(channelID, rawID)
	slog.WarnContext(ctx, "Shorts freshness unresolved after max attempts; absorbing silently without notification",
		logschema.FieldChannelID, channelID,
		"video_id", rawID,
		"attempts", attempts,
		"reason", reason,
	)
	return classifiedShortCandidate{short: short, class: shortCandidateStoreSilently}
}

func (p *ShortsPoller) resolveShortPublishedAt(ctx context.Context, channelID, videoID string) *time.Time {
	publishedAt, err := p.client.GetShortPublishedAt(ctx, channelID, videoID)
	if err != nil {
		slog.WarnContext(ctx, "Shorts published_at resolve failed",
			logschema.FieldChannelID, channelID,
			"video_id", videoID,
			"error", err.Error(),
		)
		return nil
	}
	return yttimestamp.NormalizePtr(publishedAt)
}

func shortRawResourceIDs(shorts []*scraper.Short) []string {
	ids := make([]string, 0, len(shorts))
	seen := make(map[string]struct{}, len(shorts))
	for _, short := range shorts {
		if short == nil {
			continue
		}
		rawID := polling.NormalizeShortVideoResourceID(short.VideoID)
		if rawID == "" {
			continue
		}
		if _, ok := seen[rawID]; ok {
			continue
		}
		seen[rawID] = struct{}{}
		ids = append(ids, rawID)
	}
	return ids
}

func shortRawResourceIDSet(shorts []*scraper.Short) map[string]struct{} {
	set := make(map[string]struct{}, len(shorts))
	for _, id := range shortRawResourceIDs(shorts) {
		set[id] = struct{}{}
	}
	return set
}
