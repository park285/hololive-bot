package tracking

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

const communityShortsObservationWindowDuration = 24 * time.Hour

func (r *GormRepository) FindCommunityShortsObservationWindow(
	ctx context.Context,
	runtimeName string,
	bigBangCutoverAt time.Time,
) (*domain.YouTubeCommunityShortsObservationWindow, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("find community shorts observation window: db is nil")
	}

	normalizedRuntimeName := strings.TrimSpace(runtimeName)
	if normalizedRuntimeName == "" {
		return nil, fmt.Errorf("find community shorts observation window: runtime name is empty")
	}
	if bigBangCutoverAt.IsZero() {
		return nil, fmt.Errorf("find community shorts observation window: big-bang cutover at is empty")
	}

	var record domain.YouTubeCommunityShortsObservationWindow
	if err := r.db.WithContext(ctx).
		Where("runtime_name = ? AND bigbang_cutover_at = ?", normalizedRuntimeName, yttimestamp.Normalize(bigBangCutoverAt)).
		First(&record).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("find community shorts observation window: query row: %w", err)
	}

	return &record, nil
}

func (r *GormRepository) FindClosedCommunityShortsObservationWindow(
	ctx context.Context,
	runtimeName string,
	bigBangCutoverAt time.Time,
	now time.Time,
) (*domain.YouTubeCommunityShortsObservationWindow, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("find closed community shorts observation window: db is nil")
	}

	normalizedNow := yttimestamp.Normalize(now)
	if normalizedNow.IsZero() {
		return nil, fmt.Errorf("find closed community shorts observation window: now is empty")
	}

	window, err := r.FindCommunityShortsObservationWindow(ctx, runtimeName, bigBangCutoverAt)
	if err != nil {
		return nil, fmt.Errorf("find closed community shorts observation window: %w", err)
	}
	if window == nil {
		return nil, nil
	}

	if !normalizedNow.Before(window.ObservationEndedAt.UTC()) {
		if err := r.closeCommunityShortsObservationWindow(ctx, window.RuntimeName, window.BigBangCutoverAt, window.ObservationEndedAt); err != nil {
			return nil, fmt.Errorf("find closed community shorts observation window: close due window: %w", err)
		}
		window, err = r.FindCommunityShortsObservationWindow(ctx, runtimeName, bigBangCutoverAt)
		if err != nil {
			return nil, fmt.Errorf("find closed community shorts observation window: reload row: %w", err)
		}
		if window == nil {
			return nil, nil
		}
	}

	if !communityShortsObservationWindowClosed(window) {
		return nil, fmt.Errorf(
			"find closed community shorts observation window: observation window is still open until %s",
			window.ObservationEndedAt.UTC().Format(time.RFC3339),
		)
	}
	if err := r.ensureCommunityShortsObservationPostBaselines(ctx, window); err != nil {
		return nil, fmt.Errorf("find closed community shorts observation window: finalize observation post baseline: %w", err)
	}

	window, err = r.FindCommunityShortsObservationWindow(ctx, runtimeName, bigBangCutoverAt)
	if err != nil {
		return nil, fmt.Errorf("find closed community shorts observation window: reload finalized row: %w", err)
	}
	if window == nil {
		return nil, nil
	}

	return window, nil
}

func (r *GormRepository) EnsureCommunityShortsObservationWindow(
	ctx context.Context,
	window *domain.YouTubeCommunityShortsObservationWindow,
) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("ensure community shorts observation window: db is nil")
	}

	normalizedWindow, err := normalizeCommunityShortsObservationWindow(window)
	if err != nil {
		return fmt.Errorf("ensure community shorts observation window: %w", err)
	}

	tableName := normalizedWindow.TableName()
	updates := clause.Assignments(map[string]any{
		"app_version": gorm.Expr(
			"CASE WHEN " + tableName + ".app_version = '' THEN excluded.app_version ELSE " + tableName + ".app_version END",
		),
		"target_channel_count": gorm.Expr(
			"CASE WHEN excluded.target_channel_count > " + tableName + ".target_channel_count THEN excluded.target_channel_count ELSE " + tableName + ".target_channel_count END",
		),
		"deployment_completed_at": gorm.Expr(
			"CASE WHEN excluded.deployment_completed_at < " + tableName + ".deployment_completed_at THEN excluded.deployment_completed_at ELSE " + tableName + ".deployment_completed_at END",
		),
		"observation_started_at": gorm.Expr(
			"CASE WHEN excluded.observation_started_at < " + tableName + ".observation_started_at THEN excluded.observation_started_at ELSE " + tableName + ".observation_started_at END",
		),
		"observation_ended_at": gorm.Expr(
			"CASE WHEN excluded.observation_started_at < " + tableName + ".observation_started_at THEN excluded.observation_ended_at ELSE " + tableName + ".observation_ended_at END",
		),
		"updated_at": yttimestamp.Normalize(time.Now()),
	})

	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "runtime_name"}, {Name: "bigbang_cutover_at"}},
			DoUpdates: updates,
		}).
		Create(normalizedWindow).Error; err != nil {
		return fmt.Errorf("ensure community shorts observation window: upsert row: %w", err)
	}

	return nil
}

func (r *GormRepository) closeCommunityShortsObservationWindow(
	ctx context.Context,
	runtimeName string,
	bigBangCutoverAt time.Time,
	closedAt time.Time,
) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("close community shorts observation window: db is nil")
	}

	normalizedRuntimeName := strings.TrimSpace(runtimeName)
	if normalizedRuntimeName == "" {
		return fmt.Errorf("close community shorts observation window: runtime name is empty")
	}

	normalizedCutoverAt := yttimestamp.Normalize(bigBangCutoverAt)
	if normalizedCutoverAt.IsZero() {
		return fmt.Errorf("close community shorts observation window: big-bang cutover at is empty")
	}

	normalizedClosedAt := yttimestamp.Normalize(closedAt)
	if normalizedClosedAt.IsZero() {
		return fmt.Errorf("close community shorts observation window: closed at is empty")
	}

	if err := r.db.WithContext(ctx).
		Model(&domain.YouTubeCommunityShortsObservationWindow{}).
		Where("runtime_name = ? AND bigbang_cutover_at = ?", normalizedRuntimeName, normalizedCutoverAt).
		Where("closed_at IS NULL").
		Where("observation_ended_at <= ?", normalizedClosedAt).
		Updates(map[string]any{
			"closed_at":  normalizedClosedAt,
			"updated_at": normalizedClosedAt,
		}).Error; err != nil {
		return fmt.Errorf("close community shorts observation window: update row: %w", err)
	}

	return nil
}

func normalizeCommunityShortsObservationWindow(
	window *domain.YouTubeCommunityShortsObservationWindow,
) (*domain.YouTubeCommunityShortsObservationWindow, error) {
	if window == nil {
		return nil, fmt.Errorf("window is nil")
	}

	runtimeName := strings.TrimSpace(window.RuntimeName)
	if runtimeName == "" {
		return nil, fmt.Errorf("runtime name is empty")
	}

	appVersion := strings.TrimSpace(window.AppVersion)
	if appVersion == "" {
		return nil, fmt.Errorf("app version is empty")
	}

	bigBangCutoverAt := yttimestamp.Normalize(window.BigBangCutoverAt)
	if bigBangCutoverAt.IsZero() {
		return nil, fmt.Errorf("big-bang cutover at is empty")
	}

	deploymentCompletedAt := yttimestamp.Normalize(window.DeploymentCompletedAt)
	if deploymentCompletedAt.IsZero() {
		return nil, fmt.Errorf("deployment completed at is empty")
	}

	observationStartedAt := yttimestamp.Normalize(window.ObservationStartedAt)
	if observationStartedAt.IsZero() {
		return nil, fmt.Errorf("observation started at is empty")
	}

	observationEndedAt := yttimestamp.Normalize(window.ObservationEndedAt)
	if observationEndedAt.IsZero() {
		return nil, fmt.Errorf("observation ended at is empty")
	}
	if !observationEndedAt.After(observationStartedAt) {
		return nil, fmt.Errorf("observation ended at must be after observation started at")
	}
	if observationEndedAt.Sub(observationStartedAt) != communityShortsObservationWindowDuration {
		return nil, fmt.Errorf("observation window duration must be exactly 24h")
	}
	if !deploymentCompletedAt.Equal(observationStartedAt) {
		return nil, fmt.Errorf("deployment completed at must match observation started at")
	}
	if window.TargetChannelCount <= 0 {
		return nil, fmt.Errorf("target channel count must be greater than zero")
	}

	closedAt, err := normalizeCommunityShortsObservationWindowClosedAt(window.ClosedAt, observationEndedAt)
	if err != nil {
		return nil, err
	}
	finalizedPostBaselineAt, err := normalizeCommunityShortsObservationFinalizedAt(window.FinalizedPostBaselineAt, observationEndedAt)
	if err != nil {
		return nil, err
	}
	if window.FinalizedPostCount < 0 {
		return nil, fmt.Errorf("finalized post count must not be negative")
	}
	if finalizedPostBaselineAt != nil && closedAt == nil {
		return nil, fmt.Errorf("finalized post baseline at requires closed at")
	}
	if finalizedPostBaselineAt == nil && window.FinalizedPostCount != 0 {
		return nil, fmt.Errorf("finalized post count requires finalized post baseline at")
	}

	return &domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName:             runtimeName,
		BigBangCutoverAt:        bigBangCutoverAt,
		AppVersion:              appVersion,
		TargetChannelCount:      window.TargetChannelCount,
		DeploymentCompletedAt:   deploymentCompletedAt,
		ObservationStartedAt:    observationStartedAt,
		ObservationEndedAt:      observationEndedAt,
		ClosedAt:                closedAt,
		FinalizedPostBaselineAt: finalizedPostBaselineAt,
		FinalizedPostCount:      window.FinalizedPostCount,
	}, nil
}

func normalizeCommunityShortsObservationWindowClosedAt(
	closedAt *time.Time,
	observationEndedAt time.Time,
) (*time.Time, error) {
	if closedAt == nil {
		return nil, nil
	}

	normalizedClosedAt := yttimestamp.Normalize(*closedAt)
	if normalizedClosedAt.IsZero() {
		return nil, nil
	}
	if !normalizedClosedAt.Equal(observationEndedAt) {
		return nil, fmt.Errorf("closed at must match observation ended at")
	}
	return &normalizedClosedAt, nil
}

func normalizeCommunityShortsObservationFinalizedAt(
	finalizedAt *time.Time,
	observationEndedAt time.Time,
) (*time.Time, error) {
	if finalizedAt == nil {
		return nil, nil
	}

	normalizedFinalizedAt := yttimestamp.Normalize(*finalizedAt)
	if normalizedFinalizedAt.IsZero() {
		return nil, nil
	}
	if !normalizedFinalizedAt.Equal(observationEndedAt) {
		return nil, fmt.Errorf("finalized post baseline at must match observation ended at")
	}
	return &normalizedFinalizedAt, nil
}

func communityShortsObservationWindowClosed(window *domain.YouTubeCommunityShortsObservationWindow) bool {
	if window == nil || window.ClosedAt == nil || window.ClosedAt.IsZero() {
		return false
	}
	return window.ClosedAt.UTC().Equal(window.ObservationEndedAt.UTC())
}
