package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/viewer"
)

func lockViewerSubject(ctx context.Context, tx dbx.Tx, videoID string) error {
	return lockLiveSubject(ctx, tx, "viewer:"+videoID)
}

func loadViewerState(ctx context.Context, tx dbx.Tx, videoID string, windowStart time.Time) (viewer.State, error) {
	state := viewer.State{VideoID: videoID, Head: viewer.Head{VideoID: videoID}}
	err := tx.QueryRow(ctx, mustSQL("repository_viewer_head_0052_52.sql"), videoID).Scan(
		&state.Head.VideoID,
		&state.Head.LastResolvedWindowStart,
		&state.Head.LastResolvedCount,
		&state.Head.LastResolvedAvailability,
		&state.Head.PriorResolvedWindowStart,
		&state.Head.PriorResolvedCount,
		&state.Head.PriorResolvedAvailability,
		&state.Head.UnresolvedWindowStart,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return viewer.State{}, fmt.Errorf("load viewer head: %w", err)
	}
	rows, err := tx.Query(ctx, mustSQL("repository_viewer_window_0053_53.sql"), videoID, windowStart)
	if err != nil {
		return viewer.State{}, fmt.Errorf("load viewer window: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item viewer.WindowEvidence
		if err := rows.Scan(&item.Provider, &item.ViewerCount, &item.Availability); err != nil {
			return viewer.State{}, fmt.Errorf("scan viewer window: %w", err)
		}
		digest, err := viewer.SampleDigest(item.Availability, item.ViewerCount)
		if err != nil {
			return viewer.State{}, err
		}
		item.Digest = digest
		state.Window = append(state.Window, item)
	}
	return state, rows.Err()
}

func persistViewerDecision(ctx context.Context, tx dbx.Tx, observation *Observation, decision *viewer.Decision, channelID string) error {
	if err := persistViewerEvidence(ctx, tx, observation, decision); err != nil {
		return err
	}
	if err := persistViewerHead(ctx, tx, decision); err != nil {
		return err
	}
	if err := persistViewerProduct(ctx, tx, decision, channelID); err != nil {
		return err
	}
	return persistViewerConflict(ctx, tx, observation, decision)
}

func persistViewerEvidence(ctx context.Context, tx dbx.Tx, observation *Observation, decision *viewer.Decision) error {
	if decision.Sample == nil {
		return nil
	}
	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_viewer_evidence_upsert_0054_54.sql"),
		decision.Sample.VideoID,
		decision.Sample.WindowStart,
		observation.Provider,
		observation.ID,
		decision.Sample.ViewerCount,
		decision.Sample.Availability,
		decision.Sample.WindowSeconds,
		decision.Sample.ScheduledFor,
		decision.Sample.EffectiveAt,
		decision.Sample.ReceivedAt,
	); err != nil {
		return fmt.Errorf("upsert viewer evidence: %w", err)
	}
	return nil
}

func persistViewerHead(ctx context.Context, tx dbx.Tx, decision *viewer.Decision) error {
	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_viewer_head_upsert_0055_55.sql"),
		decision.Head.VideoID,
		decision.Head.LastResolvedWindowStart,
		decision.Head.LastResolvedCount,
		decision.Head.LastResolvedAvailability,
		decision.Head.PriorResolvedWindowStart,
		decision.Head.PriorResolvedCount,
		decision.Head.PriorResolvedAvailability,
		decision.Head.UnresolvedWindowStart,
	); err != nil {
		return fmt.Errorf("upsert viewer head: %w", err)
	}
	return nil
}

func persistViewerProduct(ctx context.Context, tx dbx.Tx, decision *viewer.Decision, channelID string) error {
	if decision.Sample == nil {
		return nil
	}
	if decision.ClearProduct {
		if _, err := tx.Exec(
			ctx,
			mustSQL("repository_viewer_product_delete_0057_57.sql"),
			decision.Sample.VideoID,
			decision.Sample.WindowStart,
		); err != nil {
			return fmt.Errorf("clear unresolved viewer product: %w", err)
		}
		return nil
	}
	if !shouldWriteViewerProduct(decision, channelID) {
		return nil
	}
	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_viewer_product_upsert_0056_56.sql"),
		decision.Sample.VideoID,
		decision.Sample.WindowStart,
		channelID,
		int(*decision.Sample.ViewerCount),
	); err != nil {
		return fmt.Errorf("upsert viewer product sample: %w", err)
	}
	return nil
}

func shouldWriteViewerProduct(decision *viewer.Decision, channelID string) bool {
	return decision.Sample.Availability == viewer.AvailabilityAvailable &&
		decision.Sample.ViewerCount != nil &&
		channelID != ""
}

func persistViewerConflict(ctx context.Context, tx dbx.Tx, observation *Observation, decision *viewer.Decision) error {
	if decision.Conflict == nil || decision.Sample == nil {
		return nil
	}
	if err := persistReconcileConflict(ctx, tx, observation, "youtube_live_viewer_sample", decision.Sample.VideoID, decision.Conflict.FieldName, decision.Conflict.ExistingValueSHA256, decision.Conflict.AttemptedValueSHA256, "UNRESOLVED"); err != nil {
		return fmt.Errorf("insert viewer reconciliation conflict: %w", err)
	}
	return nil
}

func loadViewerChannelID(ctx context.Context, tx dbx.Tx, videoID string) (string, error) {
	var channelID string
	err := tx.QueryRow(ctx, mustSQL("repository_viewer_channel_0062_62.sql"), videoID).Scan(&channelID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load viewer channel: %w", err)
	}
	return channelID, nil
}
