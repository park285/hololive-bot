package pollers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

const (
	shortsFreshnessHorizon     = 72 * time.Hour
	shortsFreshnessFutureSkew  = time.Hour
	shortsFreshnessMaxAttempts = 3
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

type shortsFreshnessDeferrals struct {
	mu       sync.Mutex
	attempts map[string]int
}

func newShortsFreshnessDeferrals() *shortsFreshnessDeferrals {
	return &shortsFreshnessDeferrals{attempts: make(map[string]int)}
}

func (d *shortsFreshnessDeferrals) recordFailure(channelID, videoID string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	key := channelID + "|" + videoID
	d.attempts[key]++
	return d.attempts[key]
}

func (d *shortsFreshnessDeferrals) clear(channelID, videoID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.attempts, channelID+"|"+videoID)
}

// 목록에서 사라진 보류 항목도 상한까지 시도 횟수를 올리며 붙잡는다. 바로 지우면
// watermark가 전진해, 항목이 watermark 아래로 재등장할 때 영구 유실된다.
func (d *shortsFreshnessDeferrals) reconcileChannel(
	channelID string,
	scrapedVideoIDs map[string]struct{},
	deferredVideoIDs map[string]struct{},
) (holdWatermark bool, departed []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	prefix := channelID + "|"
	for key, attempts := range d.attempts {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		videoID := strings.TrimPrefix(key, prefix)
		if _, stillDeferred := deferredVideoIDs[videoID]; stillDeferred {
			holdWatermark = true
			continue
		}
		if _, scraped := scrapedVideoIDs[videoID]; scraped {
			delete(d.attempts, key)
			continue
		}
		attempts++
		if attempts >= shortsFreshnessMaxAttempts {
			delete(d.attempts, key)
			departed = append(departed, videoID)
			continue
		}
		d.attempts[key] = attempts
		holdWatermark = true
	}
	return holdWatermark, departed
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
	historical := publishedAt == nil && now.Sub(state.FirstSeenAt) > shortsFreshnessHorizon
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
	case age > shortsFreshnessHorizon:
		p.deferrals.clear(channelID, rawID)
		return classifiedShortCandidate{short: short, class: shortCandidateStoreSilently, publishedAt: publishedAt}
	case age >= -shortsFreshnessFutureSkew:
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
	if attempts < shortsFreshnessMaxAttempts {
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
