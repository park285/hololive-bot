package observation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

const communityShortsObservationWindowDuration = 24 * time.Hour

func (r *windowRepository) FindCommunityShortsObservationWindow(
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
	found, err := getSQL(ctx, r.db, &record, "find community shorts observation window: query row", `
		SELECT runtime_name, bigbang_cutover_at, app_version, target_channel_count,
		       deployment_completed_at, observation_started_at, observation_ended_at,
		       closed_at, finalized_post_baseline_at, finalized_post_count, created_at, updated_at
		FROM youtube_community_shorts_observation_windows
		WHERE runtime_name = ? AND bigbang_cutover_at = ?
		LIMIT 1
	`, normalizedRuntimeName, yttimestamp.Normalize(bigBangCutoverAt))
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	return &record, nil
}

func (r *windowRepository) FindClosedCommunityShortsObservationWindow(
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

func (r *windowRepository) requireCommunityShortsObservationWindowDB(action string) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("%s: db is nil", action)
	}
	return nil
}

func (r *windowRepository) closeDueCommunityShortsObservationWindow(
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

func (r *windowRepository) finalizeCommunityShortsObservationWindow(
	ctx context.Context,
	runtimeName string,
	bigBangCutoverAt time.Time,
	window *domain.YouTubeCommunityShortsObservationWindow,
) (*domain.YouTubeCommunityShortsObservationWindow, error) {
	finalizeErr := r.owner.baseline.ensureCommunityShortsObservationPostBaselines(ctx, window)
	if finalizeErr != nil {
		return nil, fmt.Errorf("finalize observation post baseline: %w", finalizeErr)
	}

	window, err := r.FindCommunityShortsObservationWindow(ctx, runtimeName, bigBangCutoverAt)
	if err != nil {
		return nil, fmt.Errorf("reload finalized row: %w", err)
	}

	return window, nil
}

func (r *windowRepository) EnsureCommunityShortsObservationWindow(
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

	now := yttimestamp.Normalize(time.Now())
	if _, err := execSQL(ctx, r.db, "ensure community shorts observation window: upsert row", `
		INSERT INTO youtube_community_shorts_observation_windows
			(runtime_name, bigbang_cutover_at, app_version, target_channel_count, deployment_completed_at,
			 observation_started_at, observation_ended_at, closed_at, finalized_post_baseline_at,
			 finalized_post_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (runtime_name, bigbang_cutover_at) DO UPDATE
		SET app_version = CASE
		        WHEN youtube_community_shorts_observation_windows.app_version = '' THEN EXCLUDED.app_version
		        ELSE youtube_community_shorts_observation_windows.app_version
		    END,
		    target_channel_count = CASE
		        WHEN EXCLUDED.target_channel_count > youtube_community_shorts_observation_windows.target_channel_count THEN EXCLUDED.target_channel_count
		        ELSE youtube_community_shorts_observation_windows.target_channel_count
		    END,
		    deployment_completed_at = CASE
		        WHEN EXCLUDED.deployment_completed_at < youtube_community_shorts_observation_windows.deployment_completed_at THEN EXCLUDED.deployment_completed_at
		        ELSE youtube_community_shorts_observation_windows.deployment_completed_at
		    END,
		    observation_started_at = CASE
		        WHEN EXCLUDED.observation_started_at < youtube_community_shorts_observation_windows.observation_started_at THEN EXCLUDED.observation_started_at
		        ELSE youtube_community_shorts_observation_windows.observation_started_at
		    END,
		    observation_ended_at = CASE
		        WHEN EXCLUDED.observation_started_at < youtube_community_shorts_observation_windows.observation_started_at THEN EXCLUDED.observation_ended_at
		        ELSE youtube_community_shorts_observation_windows.observation_ended_at
		    END,
		    updated_at = EXCLUDED.updated_at
	`, normalizedWindow.RuntimeName, normalizedWindow.BigBangCutoverAt, normalizedWindow.AppVersion,
		normalizedWindow.TargetChannelCount, normalizedWindow.DeploymentCompletedAt,
		normalizedWindow.ObservationStartedAt, normalizedWindow.ObservationEndedAt,
		normalizedWindow.ClosedAt, normalizedWindow.FinalizedPostBaselineAt,
		normalizedWindow.FinalizedPostCount, now, now); err != nil {
		return err
	}

	return nil
}

func (r *windowRepository) closeCommunityShortsObservationWindow(
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

	if _, err := execSQL(ctx, r.db, "close community shorts observation window: update row", `
		UPDATE youtube_community_shorts_observation_windows
		SET closed_at = ?, updated_at = ?
		WHERE runtime_name = ? AND bigbang_cutover_at = ?
		  AND closed_at IS NULL
		  AND observation_ended_at <= ?
	`, normalizedClosedAt, normalizedClosedAt, normalizedRuntimeName, normalizedCutoverAt, normalizedClosedAt); err != nil {
		return err
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
