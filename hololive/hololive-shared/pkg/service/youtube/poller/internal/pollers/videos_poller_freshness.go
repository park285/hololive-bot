package pollers

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/internal/dbx"
	yttimestamp "github.com/kapu/hololive-shared/internal/service/youtube/timestamp"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type videoFreshness int

const (
	videoFreshnessUnresolved videoFreshness = iota
	videoFreshnessFresh
	videoFreshnessHistorical
)

type videoPublicationEvidence struct {
	freshness   videoFreshness
	publishedAt *time.Time
}

var videoRelativePublishedText = regexp.MustCompile(`(?i)^(?:uploaded\s+)?(\d+)\s*(s|sec|secs|second|seconds|m|min|mins|minute|minutes|h|hr|hrs|hour|hours|d|day|days|w|wk|wks|week|weeks|mo|mos|month|months|y|yr|yrs|year|years)\s+ago$`)

var videoRelativeTimeUnits = map[string]time.Duration{
	"s": time.Second, "sec": time.Second, "secs": time.Second, "second": time.Second, "seconds": time.Second,
	"m": time.Minute, "min": time.Minute, "mins": time.Minute, "minute": time.Minute, "minutes": time.Minute,
	"h": time.Hour, "hr": time.Hour, "hrs": time.Hour, "hour": time.Hour, "hours": time.Hour,
	"d": 24 * time.Hour, "day": 24 * time.Hour, "days": 24 * time.Hour,
	"w": 7 * 24 * time.Hour, "wk": 7 * 24 * time.Hour, "wks": 7 * 24 * time.Hour, "week": 7 * 24 * time.Hour, "weeks": 7 * 24 * time.Hour,
	"mo": 28 * 24 * time.Hour, "mos": 28 * 24 * time.Hour, "month": 28 * 24 * time.Hour, "months": 28 * 24 * time.Hour,
	"y": 365 * 24 * time.Hour, "yr": 365 * 24 * time.Hour, "yrs": 365 * 24 * time.Hour, "year": 365 * 24 * time.Hour, "years": 365 * 24 * time.Hour,
}

type knownVideoIDRow struct {
	VideoID string `db:"video_id"`
}

func loadKnownVideoIDs(ctx context.Context, db dbx.Querier, videoIDs []string) (map[string]struct{}, error) {
	known := make(map[string]struct{}, len(videoIDs))
	if len(videoIDs) == 0 {
		return known, nil
	}

	var rows []knownVideoIDRow
	query := mustSQL("videos_poller_freshness_0038_01.sql") + dbx.InPlaceholders(len(videoIDs)) + `)`
	if err := dbx.SelectSQL(ctx, db, &rows, "query known video ids", query, dbx.AnyArgs(videoIDs)...); err != nil {
		return nil, fmt.Errorf("query known video ids: %w", err)
	}
	for i := range rows {
		known[rows[i].VideoID] = struct{}{}
	}
	return known, nil
}

func collectedVideoIDs(videos []*scraper.Video) []string {
	ids := make([]string, 0, len(videos))
	seen := make(map[string]struct{}, len(videos))
	for _, video := range videos {
		if video == nil || strings.TrimSpace(video.VideoID) == "" {
			continue
		}
		if _, ok := seen[video.VideoID]; ok {
			continue
		}
		seen[video.VideoID] = struct{}{}
		ids = append(ids, video.VideoID)
	}
	return ids
}

func collectedVideoIDSet(videos []*scraper.Video) map[string]struct{} {
	ids := collectedVideoIDs(videos)
	set := make(map[string]struct{}, len(ids))
	for _, videoID := range ids {
		set[videoID] = struct{}{}
	}
	return set
}

func videoPublishedTextEvidence(publishedText string, now time.Time) videoPublicationEvidence {
	if publishedAt, ok := yttimestamp.ParsePublishedAt(publishedText); ok {
		return videoPublicationEvidence{
			freshness:   classifyVideoPublishedAt(*publishedAt, now),
			publishedAt: publishedAt,
		}
	}

	matches := videoRelativePublishedText.FindStringSubmatch(strings.TrimSpace(publishedText))
	if len(matches) != 3 {
		return videoPublicationEvidence{freshness: videoFreshnessUnresolved}
	}
	count, err := strconv.ParseInt(matches[1], 10, 64)
	if err != nil {
		return videoPublicationEvidence{freshness: videoFreshnessUnresolved}
	}
	unit, ok := videoRelativeTimeUnit(matches[2])
	if !ok {
		return videoPublicationEvidence{freshness: videoFreshnessUnresolved}
	}
	return videoPublicationEvidence{freshness: classifyVideoRelativeAge(count, unit)}
}

func classifyVideoPublishedAt(publishedAt, now time.Time) videoFreshness {
	age := now.Sub(publishedAt)
	switch {
	case age > publicationFreshnessHorizon:
		return videoFreshnessHistorical
	case age >= -publicationFreshnessFutureSkew:
		return videoFreshnessFresh
	default:
		return videoFreshnessUnresolved
	}
}

func classifyVideoRelativeAge(count int64, unit time.Duration) videoFreshness {
	if count < 0 || unit <= 0 {
		return videoFreshnessUnresolved
	}
	maxRelevantCount := int64(publicationFreshnessHorizon/unit) + 1
	if count > maxRelevantCount {
		return videoFreshnessHistorical
	}
	lowerBound := time.Duration(count) * unit
	upperBound := time.Duration(count+1) * unit
	switch {
	case lowerBound > publicationFreshnessHorizon:
		return videoFreshnessHistorical
	case upperBound <= publicationFreshnessHorizon:
		return videoFreshnessFresh
	default:
		return videoFreshnessUnresolved
	}
}

func videoRelativeTimeUnit(unit string) (time.Duration, bool) {
	duration, ok := videoRelativeTimeUnits[strings.ToLower(unit)]

	return duration, ok
}
