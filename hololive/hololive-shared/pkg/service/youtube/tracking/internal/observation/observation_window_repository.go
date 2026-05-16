package observation

import (
	"context"
	"errors"
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
		if errors.Is(err, gorm.ErrRecordNotFound) {
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
	if err := r.requireCommunityShortsObservationWindowDB("find closed community shorts observation window"); err != nil {
		return nil, err
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

	window, err = r.closeDueCommunityShortsObservationWindow(ctx, runtimeName, bigBangCutoverAt, normalizedNow, window)
	if err != nil {
		return nil, fmt.Errorf("find closed community shorts observation window: %w", err)
	}
	if window == nil {
		return nil, nil
	}

	if err := requireClosedCommunityShortsObservationWindow(window); err != nil {
		return nil, fmt.Errorf("find closed community shorts observation window: %w", err)
	}

	window, err = r.finalizeCommunityShortsObservationWindow(ctx, runtimeName, bigBangCutoverAt, window)
	if err != nil {
		return nil, fmt.Errorf("find closed community shorts observation window: %w", err)
	}

	return window, nil
}

func (r *GormRepository) requireCommunityShortsObservationWindowDB(action string) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("%s: db is nil", action)
	}
	return nil
}

func (r *GormRepository) closeDueCommunityShortsObservationWindow(
	ctx context.Context,
	runtimeName string,
	bigBangCutoverAt time.Time,
	normalizedNow time.Time,
	window *domain.YouTubeCommunityShortsObservationWindow,
) (*domain.YouTubeCommunityShortsObservationWindow, error) {
	if normalizedNow.Before(window.ObservationEndedAt.UTC()) {
		return window, nil
	}

	closeErr := r.closeCommunityShortsObservationWindow(ctx, window.RuntimeName, window.BigBangCutoverAt, window.ObservationEndedAt)
	if closeErr != nil {
		return nil, fmt.Errorf("close due window: %w", closeErr)
	}

	window, err := r.FindCommunityShortsObservationWindow(ctx, runtimeName, bigBangCutoverAt)
	if err != nil {
		return nil, fmt.Errorf("reload row: %w", err)
	}

	return window, nil
}

func requireClosedCommunityShortsObservationWindow(window *domain.YouTubeCommunityShortsObservationWindow) error {
	if communityShortsObservationWindowClosed(window) {
		return nil
	}
	return fmt.Errorf(
		"observation window is still open until %s",
		window.ObservationEndedAt.UTC().Format(time.RFC3339),
	)
}

func (r *GormRepository) finalizeCommunityShortsObservationWindow(
	ctx context.Context,
	runtimeName string,
	bigBangCutoverAt time.Time,
	window *domain.YouTubeCommunityShortsObservationWindow,
) (*domain.YouTubeCommunityShortsObservationWindow, error) {
	finalizeErr := r.ensureCommunityShortsObservationPostBaselines(ctx, window)
	if finalizeErr != nil {
		return nil, fmt.Errorf("finalize observation post baseline: %w", finalizeErr)
	}

	window, err := r.FindCommunityShortsObservationWindow(ctx, runtimeName, bigBangCutoverAt)
	if err != nil {
		return nil, fmt.Errorf("reload finalized row: %w", err)
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

	text, err := normalizeCommunityShortsObservationWindowText(window)
	if err != nil {
		return nil, err
	}

	times, normalizeErr := normalizeCommunityShortsObservationWindowTimes(window)
	if normalizeErr != nil {
		return nil, normalizeErr
	}
	if err := validateCommunityShortsObservationWindowTiming(
		times.deploymentCompletedAt,
		times.observationStartedAt,
		times.observationEndedAt,
	); err != nil {
		return nil, err
	}
	if window.TargetChannelCount <= 0 {
		return nil, fmt.Errorf("target channel count must be greater than zero")
	}

	closedAt, err := normalizeCommunityShortsObservationWindowClosedAt(window.ClosedAt, times.observationEndedAt)
	if err != nil {
		return nil, err
	}
	finalizedPostBaselineAt, err := normalizeCommunityShortsObservationFinalizedAt(window.FinalizedPostBaselineAt, times.observationEndedAt)
	if err != nil {
		return nil, err
	}
	if err := validateCommunityShortsObservationWindowFinalization(window, closedAt, finalizedPostBaselineAt); err != nil {
		return nil, err
	}

	return &domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName:             text.runtimeName,
		BigBangCutoverAt:        times.bigBangCutoverAt,
		AppVersion:              text.appVersion,
		TargetChannelCount:      window.TargetChannelCount,
		DeploymentCompletedAt:   times.deploymentCompletedAt,
		ObservationStartedAt:    times.observationStartedAt,
		ObservationEndedAt:      times.observationEndedAt,
		ClosedAt:                closedAt,
		FinalizedPostBaselineAt: finalizedPostBaselineAt,
		FinalizedPostCount:      window.FinalizedPostCount,
	}, nil
}
