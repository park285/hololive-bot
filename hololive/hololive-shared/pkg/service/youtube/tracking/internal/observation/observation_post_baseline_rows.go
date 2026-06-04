package observation

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func buildCommunityShortsObservationPostBaselines(
	window *domain.YouTubeCommunityShortsObservationWindow,
	sourcePosts []domain.YouTubeCommunityShortsSourcePost,
) ([]*domain.YouTubeCommunityShortsObservationPostBaseline, error) {
	normalizedWindow, err := normalizeCommunityShortsObservationWindow(window)
	if err != nil {
		return nil, fmt.Errorf("normalize observation window: %w", err)
	}

	rows := make([]*domain.YouTubeCommunityShortsObservationPostBaseline, 0, len(sourcePosts))
	for i := range sourcePosts {
		row, err := normalizeCommunityShortsObservationPostBaseline(&domain.YouTubeCommunityShortsObservationPostBaseline{
			RuntimeName:       normalizedWindow.RuntimeName,
			BigBangCutoverAt:  normalizedWindow.BigBangCutoverAt,
			Kind:              sourcePosts[i].Kind,
			PostID:            sourcePosts[i].PostID,
			ChannelID:         sourcePosts[i].ChannelID,
			ActualPublishedAt: sourcePosts[i].ActualPublishedAt,
			DetectedAt:        sourcePosts[i].DetectedAt,
			FinalizedAt:       normalizedWindow.ObservationEndedAt,
		})
		if err != nil {
			return nil, fmt.Errorf("normalize source post at index %d: %w", i, err)
		}
		rows = append(rows, row)
	}

	return rows, nil
}

func buildObservationPostBaselineUpsert(
	normalized []*domain.YouTubeCommunityShortsObservationPostBaseline,
	now time.Time,
) (string, []any) {
	args := make([]any, 0, len(normalized)*10)
	var sb strings.Builder
	sb.WriteString(`
		INSERT INTO youtube_community_shorts_observation_post_baselines
			(runtime_name, bigbang_cutover_at, kind, post_id, channel_id, actual_published_at, detected_at, finalized_at, created_at, updated_at)
		VALUES
	`)
	for i, row := range normalized {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(args,
			row.RuntimeName,
			row.BigBangCutoverAt,
			row.Kind,
			row.PostID,
			row.ChannelID,
			row.ActualPublishedAt,
			row.DetectedAt,
			row.FinalizedAt,
			now,
			now,
		)
	}
	sb.WriteString(`
		ON CONFLICT (runtime_name, bigbang_cutover_at, kind, post_id) DO UPDATE
		SET channel_id = EXCLUDED.channel_id,
		    actual_published_at = CASE
		        WHEN EXCLUDED.actual_published_at IS NULL THEN youtube_community_shorts_observation_post_baselines.actual_published_at
		        ELSE EXCLUDED.actual_published_at
		    END,
		    detected_at = CASE
		        WHEN EXCLUDED.detected_at < youtube_community_shorts_observation_post_baselines.detected_at THEN EXCLUDED.detected_at
		        ELSE youtube_community_shorts_observation_post_baselines.detected_at
		    END,
		    finalized_at = CASE
		        WHEN EXCLUDED.finalized_at < youtube_community_shorts_observation_post_baselines.finalized_at THEN EXCLUDED.finalized_at
		        ELSE youtube_community_shorts_observation_post_baselines.finalized_at
		    END,
		    updated_at = EXCLUDED.updated_at
	`)
	return sb.String(), args
}

func normalizeCommunityShortsObservationPostBaseline(
	row *domain.YouTubeCommunityShortsObservationPostBaseline,
) (*domain.YouTubeCommunityShortsObservationPostBaseline, error) {
	if row == nil {
		return nil, fmt.Errorf("row is nil")
	}

	normalizedRuntimeName, normalizedCutoverAt, err := normalizeCommunityShortsObservationWindowKey(row.RuntimeName, row.BigBangCutoverAt)
	if err != nil {
		return nil, err
	}
	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(row.Kind, row.PostID)
	if err != nil {
		return nil, err
	}

	normalizedChannelID := strings.TrimSpace(row.ChannelID)
	if normalizedChannelID == "" {
		return nil, fmt.Errorf("channel id is empty")
	}
	if row.DetectedAt.IsZero() {
		return nil, fmt.Errorf("detected at is empty")
	}
	if row.FinalizedAt.IsZero() {
		return nil, fmt.Errorf("finalized at is empty")
	}

	return &domain.YouTubeCommunityShortsObservationPostBaseline{
		RuntimeName:       normalizedRuntimeName,
		BigBangCutoverAt:  normalizedCutoverAt,
		Kind:              normalizedKind,
		PostID:            normalizedPostID,
		ChannelID:         normalizedChannelID,
		ActualPublishedAt: yttimestamp.NormalizePtr(row.ActualPublishedAt),
		DetectedAt:        yttimestamp.Normalize(row.DetectedAt),
		FinalizedAt:       yttimestamp.Normalize(row.FinalizedAt),
	}, nil
}

func normalizeCommunityShortsObservationWindowKey(runtimeName string, bigBangCutoverAt time.Time) (string, time.Time, error) {
	normalizedRuntimeName := strings.TrimSpace(runtimeName)
	if normalizedRuntimeName == "" {
		return "", time.Time{}, fmt.Errorf("runtime name is empty")
	}

	normalizedCutoverAt := yttimestamp.Normalize(bigBangCutoverAt)
	if normalizedCutoverAt.IsZero() {
		return "", time.Time{}, fmt.Errorf("big-bang cutover at is empty")
	}

	return normalizedRuntimeName, normalizedCutoverAt, nil
}

func communityShortsObservationPostBaselineFinalized(window *domain.YouTubeCommunityShortsObservationWindow) bool {
	if window == nil || window.FinalizedPostBaselineAt == nil || window.FinalizedPostBaselineAt.IsZero() {
		return false
	}
	return window.FinalizedPostBaselineAt.UTC().Equal(window.ObservationEndedAt.UTC())
}

func normalizeCommunityShortsObservationPostBaselineFinalizedArgs(
	runtimeName string,
	bigBangCutoverAt time.Time,
	finalizedAt time.Time,
	finalizedCount int,
) (string, time.Time, *time.Time, error) {
	if finalizedCount < 0 {
		return "", time.Time{}, nil, fmt.Errorf("mark community shorts observation post baseline finalized: finalized count must not be negative")
	}

	normalizedRuntimeName, normalizedCutoverAt, err := normalizeCommunityShortsObservationWindowKey(runtimeName, bigBangCutoverAt)
	if err != nil {
		return "", time.Time{}, nil, fmt.Errorf("mark community shorts observation post baseline finalized: %w", err)
	}
	finalizedAtPtr, err := normalizeCommunityShortsObservationFinalizedAt(&finalizedAt, finalizedAt.UTC())
	if err != nil {
		return "", time.Time{}, nil, fmt.Errorf("mark community shorts observation post baseline finalized: %w", err)
	}
	if finalizedAtPtr == nil {
		return "", time.Time{}, nil, fmt.Errorf("mark community shorts observation post baseline finalized: finalized at is empty")
	}

	return normalizedRuntimeName, normalizedCutoverAt, finalizedAtPtr, nil
}
