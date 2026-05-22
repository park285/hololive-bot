package observation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *GormRepository) ListCommunityShortsObservationPostBaselines(
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
	if err := r.db.WithContext(ctx).
		Where("runtime_name = ? AND bigbang_cutover_at = ?", normalizedRuntimeName, normalizedCutoverAt).
		Order("detected_at DESC").
		Order("post_id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list community shorts observation post baselines: query rows: %w", err)
	}

	return rows, nil
}

func (r *GormRepository) ensureCommunityShortsObservationPostBaselines(
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

	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return ensureCommunityShortsObservationPostBaselinesInTx(ctx, NewRepository(tx), normalizedWindow)
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
	txRepository *GormRepository,
	normalizedWindow *domain.YouTubeCommunityShortsObservationWindow,
) error {
	normalizedCurrentWindow, shouldBuild, err := reloadPendingCommunityShortsObservationPostBaselineWindow(ctx, txRepository, normalizedWindow)
	if err != nil {
		return err
	}
	if !shouldBuild {
		return nil
	}

	sourcePosts, err := txRepository.ListSourcePostsWithinObservationWindow(
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
	if err := txRepository.upsertCommunityShortsObservationPostBaselines(ctx, baselineRows); err != nil {
		return fmt.Errorf("upsert baseline rows: %w", err)
	}
	if err := txRepository.markCommunityShortsObservationPostBaselineFinalized(
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
	txRepository *GormRepository,
	normalizedWindow *domain.YouTubeCommunityShortsObservationWindow,
) (*domain.YouTubeCommunityShortsObservationWindow, bool, error) {
	currentWindow, err := txRepository.FindCommunityShortsObservationWindow(ctx, normalizedWindow.RuntimeName, normalizedWindow.BigBangCutoverAt)
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

func (r *GormRepository) upsertCommunityShortsObservationPostBaselines(
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

	tableName := normalized[0].TableName()
	updates := clause.Assignments(map[string]any{
		"channel_id": gorm.Expr("excluded.channel_id"),
		"actual_published_at": gorm.Expr(
			"CASE WHEN excluded.actual_published_at IS NULL THEN " + tableName + ".actual_published_at ELSE excluded.actual_published_at END",
		),
		"detected_at": gorm.Expr(
			"CASE WHEN excluded.detected_at < " + tableName + ".detected_at THEN excluded.detected_at ELSE " + tableName + ".detected_at END",
		),
		"finalized_at": gorm.Expr(
			"CASE WHEN excluded.finalized_at < " + tableName + ".finalized_at THEN excluded.finalized_at ELSE " + tableName + ".finalized_at END",
		),
		"updated_at": yttimestamp.Normalize(time.Now()),
	})

	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "runtime_name"}, {Name: "bigbang_cutover_at"}, {Name: "kind"}, {Name: "post_id"}},
			DoUpdates: updates,
		}).
		Create(normalized).Error; err != nil {
		return fmt.Errorf("upsert community shorts observation post baselines: upsert rows: %w", err)
	}

	return nil
}

func (r *GormRepository) markCommunityShortsObservationPostBaselineFinalized(
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

func (r *GormRepository) updateCommunityShortsObservationPostBaselineFinalized(
	ctx context.Context,
	normalizedRuntimeName string,
	normalizedCutoverAt time.Time,
	finalizedAt time.Time,
	finalizedCount int,
) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&domain.YouTubeCommunityShortsObservationWindow{}).
		Where("runtime_name = ? AND bigbang_cutover_at = ?", normalizedRuntimeName, normalizedCutoverAt).
		Where("closed_at = ?", finalizedAt).
		Where("finalized_post_baseline_at IS NULL").
		Updates(map[string]any{
			"finalized_post_baseline_at": finalizedAt,
			"finalized_post_count":       finalizedCount,
			"updated_at":                 finalizedAt,
		})
	if result.Error != nil {
		return false, fmt.Errorf("mark community shorts observation post baseline finalized: update window: %w", result.Error)
	}
	return result.RowsAffected > 0, nil
}

func (r *GormRepository) verifyCommunityShortsObservationPostBaselineFinalized(
	ctx context.Context,
	normalizedRuntimeName string,
	normalizedCutoverAt time.Time,
) error {
	window, err := r.FindCommunityShortsObservationWindow(ctx, normalizedRuntimeName, normalizedCutoverAt)
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
