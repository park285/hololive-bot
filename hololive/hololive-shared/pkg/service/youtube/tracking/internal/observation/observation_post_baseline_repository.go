package observation

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *baselineRepository) ListCommunityShortsObservationPostBaselines(
	ctx context.Context,
	runtimeName string,
	bigBangCutoverAt time.Time,
) ([]domain.YouTubeCommunityShortsObservationPostBaseline, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list community shorts observation post baselines: db is nil")
	}

	normalizedRuntimeName, normalizedCutoverAt, err := normalizeCommunityShortsObservationWindowKey(runtimeName, bigBangCutoverAt)
	if err != nil {
		return nil, fmt.Errorf("list community shorts observation post baselines: %w", err)
	}

	var rows []domain.YouTubeCommunityShortsObservationPostBaseline
	if err := selectSQL(ctx, r.db, &rows, "list community shorts observation post baselines: query rows", `
		SELECT runtime_name, bigbang_cutover_at, kind, post_id, channel_id,
		       actual_published_at, detected_at, finalized_at, created_at, updated_at
		FROM youtube_community_shorts_observation_post_baselines
		WHERE runtime_name = ? AND bigbang_cutover_at = ?
		ORDER BY detected_at DESC, post_id ASC
	`, normalizedRuntimeName, normalizedCutoverAt); err != nil {
		return nil, err
	}

	return rows, nil
}

func (r *baselineRepository) ensureCommunityShortsObservationPostBaselines(
	ctx context.Context,
	window *domain.YouTubeCommunityShortsObservationWindow,
) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("ensure community shorts observation post baselines: db is nil")
	}

	normalizedWindow, shouldBuild, err := normalizePendingCommunityShortsObservationPostBaselineWindow(window)
	if err != nil {
		return fmt.Errorf("ensure community shorts observation post baselines: %w", err)
	}
	if !shouldBuild {
		return nil
	}

	if err := inPgxTx(ctx, r.db, func(tx trackingDB) error {
		txRepo := NewRepository(tx)
		return ensureCommunityShortsObservationPostBaselinesInTx(ctx, txRepo, normalizedWindow)
	}); err != nil {
		return fmt.Errorf("ensure community shorts observation post baselines: %w", err)
	}

	return nil
}

func normalizePendingCommunityShortsObservationPostBaselineWindow(
	window *domain.YouTubeCommunityShortsObservationWindow,
) (*domain.YouTubeCommunityShortsObservationWindow, bool, error) {
	normalizedWindow, err := normalizeCommunityShortsObservationWindow(window)
	if err != nil {
		return nil, false, err
	}
	if !communityShortsObservationWindowClosed(normalizedWindow) {
		return nil, false, fmt.Errorf("observation window is not closed")
	}
	if communityShortsObservationPostBaselineFinalized(normalizedWindow) {
		return normalizedWindow, false, nil
	}

	return normalizedWindow, true, nil
}

func ensureCommunityShortsObservationPostBaselinesInTx(
	ctx context.Context,
	txRepo *PgxRepository,
	normalizedWindow *domain.YouTubeCommunityShortsObservationWindow,
) error {
	normalizedCurrentWindow, shouldBuild, err := reloadPendingCommunityShortsObservationPostBaselineWindow(ctx, txRepo, normalizedWindow)
	if err != nil {
		return err
	}
	if !shouldBuild {
		return nil
	}

	sourcePosts, err := txRepo.ListSourcePostsWithinObservationWindow(
		ctx,
		normalizedCurrentWindow.ObservationStartedAt,
		normalizedCurrentWindow.ObservationEndedAt,
		normalizedCurrentWindow.ObservationEndedAt,
	)
	if err != nil {
		return fmt.Errorf("list source posts: %w", err)
	}
	baselineRows, err := buildCommunityShortsObservationPostBaselines(normalizedCurrentWindow, sourcePosts)
	if err != nil {
		return fmt.Errorf("build baseline rows: %w", err)
	}
	if err := txRepo.baseline.upsertCommunityShortsObservationPostBaselines(ctx, baselineRows); err != nil {
		return fmt.Errorf("upsert baseline rows: %w", err)
	}
	if err := txRepo.baseline.markCommunityShortsObservationPostBaselineFinalized(
		ctx,
		normalizedCurrentWindow.RuntimeName,
		normalizedCurrentWindow.BigBangCutoverAt,
		normalizedCurrentWindow.ObservationEndedAt,
		len(baselineRows),
	); err != nil {
		return fmt.Errorf("mark finalized metadata: %w", err)
	}
	return nil
}

func reloadPendingCommunityShortsObservationPostBaselineWindow(
	ctx context.Context,
	txRepo *PgxRepository,
	normalizedWindow *domain.YouTubeCommunityShortsObservationWindow,
) (*domain.YouTubeCommunityShortsObservationWindow, bool, error) {
	currentWindow, err := txRepo.FindCommunityShortsObservationWindow(ctx, normalizedWindow.RuntimeName, normalizedWindow.BigBangCutoverAt)
	if err != nil {
		return nil, false, fmt.Errorf("reload observation window: %w", err)
	}
	if currentWindow == nil {
		return nil, false, fmt.Errorf(
			"observation window not found: runtime=%s cutover=%s",
			normalizedWindow.RuntimeName,
			normalizedWindow.BigBangCutoverAt.UTC().Format(time.RFC3339),
		)
	}

	normalizedCurrentWindow, err := normalizeCommunityShortsObservationWindow(currentWindow)
	if err != nil {
		return nil, false, fmt.Errorf("normalize observation window: %w", err)
	}
	if !communityShortsObservationWindowClosed(normalizedCurrentWindow) {
		return nil, false, fmt.Errorf("observation window is not closed")
	}
	if communityShortsObservationPostBaselineFinalized(normalizedCurrentWindow) {
		return normalizedCurrentWindow, false, nil
	}
	return normalizedCurrentWindow, true, nil
}

func (r *baselineRepository) upsertCommunityShortsObservationPostBaselines(
	ctx context.Context,
	rows []*domain.YouTubeCommunityShortsObservationPostBaseline,
) error {
	if len(rows) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("upsert community shorts observation post baselines: db is nil")
	}

	normalized := make([]*domain.YouTubeCommunityShortsObservationPostBaseline, 0, len(rows))
	for i := range rows {
		normalizedRow, err := normalizeCommunityShortsObservationPostBaseline(rows[i])
		if err != nil {
			return fmt.Errorf("upsert community shorts observation post baselines: normalize row at index %d: %w", i, err)
		}
		normalized = append(normalized, normalizedRow)
	}

	query, args := buildObservationPostBaselineUpsert(normalized, yttimestamp.Normalize(time.Now()))
	if _, err := execSQL(ctx, r.db, "upsert community shorts observation post baselines: upsert rows", query, args...); err != nil {
		return err
	}

	return nil
}

func (r *baselineRepository) markCommunityShortsObservationPostBaselineFinalized(
	ctx context.Context,
	runtimeName string,
	bigBangCutoverAt time.Time,
	finalizedAt time.Time,
	finalizedCount int,
) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("mark community shorts observation post baseline finalized: db is nil")
	}

	normalizedRuntimeName, normalizedCutoverAt, finalizedAtPtr, err := normalizeCommunityShortsObservationPostBaselineFinalizedArgs(
		runtimeName,
		bigBangCutoverAt,
		finalizedAt,
		finalizedCount,
	)
	if err != nil {
		return err
	}

	updated, err := r.updateCommunityShortsObservationPostBaselineFinalized(
		ctx,
		normalizedRuntimeName,
		normalizedCutoverAt,
		*finalizedAtPtr,
		finalizedCount,
	)
	if err != nil {
		return err
	}
	if updated {
		return nil
	}

	return r.verifyCommunityShortsObservationPostBaselineFinalized(ctx, normalizedRuntimeName, normalizedCutoverAt)
}

func (r *baselineRepository) updateCommunityShortsObservationPostBaselineFinalized(
	ctx context.Context,
	normalizedRuntimeName string,
	normalizedCutoverAt time.Time,
	finalizedAt time.Time,
	finalizedCount int,
) (bool, error) {
	rowsAffected, err := execSQL(ctx, r.db, "mark community shorts observation post baseline finalized: update window", `
		UPDATE youtube_community_shorts_observation_windows
		SET finalized_post_baseline_at = ?,
		    finalized_post_count = ?,
		    updated_at = ?
		WHERE runtime_name = ? AND bigbang_cutover_at = ?
		  AND closed_at = ?
		  AND finalized_post_baseline_at IS NULL
	`, finalizedAt, finalizedCount, finalizedAt, normalizedRuntimeName, normalizedCutoverAt, finalizedAt)
	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

func (r *baselineRepository) verifyCommunityShortsObservationPostBaselineFinalized(
	ctx context.Context,
	normalizedRuntimeName string,
	normalizedCutoverAt time.Time,
) error {
	window, err := r.owner.FindCommunityShortsObservationWindow(ctx, normalizedRuntimeName, normalizedCutoverAt)
	if err != nil {
		return fmt.Errorf("mark community shorts observation post baseline finalized: reload window: %w", err)
	}
	if window == nil {
		return fmt.Errorf("mark community shorts observation post baseline finalized: observation window not found")
	}
	if communityShortsObservationPostBaselineFinalized(window) {
		return nil
	}
	return fmt.Errorf("mark community shorts observation post baseline finalized: observation window is not closed with finalized baseline metadata")
}
