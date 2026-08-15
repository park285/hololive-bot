package sourceobservation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/stats"
)

func loadStatsState(ctx context.Context, tx dbx.Tx, channelID string, scheduledFor time.Time) (stats.State, error) {
	state := stats.State{ChannelID: channelID, Head: stats.Head{ChannelID: channelID}}
	err := tx.QueryRow(ctx, mustSQL("repository_stats_head_0065_65.sql"), channelID).Scan(
		&state.Head.ChannelID,
		&state.Head.LastResolvedScheduledFor,
		&state.Head.LastResolvedSubscriberCount,
		&state.Head.LastResolvedViewCount,
		&state.Head.LastResolvedVideoCount,
		&state.Head.PriorResolvedScheduledFor,
		&state.Head.PriorResolvedSubscriberCount,
		&state.Head.PriorResolvedViewCount,
		&state.Head.PriorResolvedVideoCount,
		&state.Head.UnresolvedScheduledFor,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return stats.State{}, fmt.Errorf("load channel stats head: %w", err)
	}
	rows, err := tx.Query(ctx, mustSQL("repository_stats_evidence_0064_64.sql"), channelID, scheduledFor)
	if err != nil {
		return stats.State{}, fmt.Errorf("load channel stats evidence: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item stats.SlotEvidence
		if err := rows.Scan(
			&item.Provider, &item.SubscriberCount, &item.ViewCount, &item.VideoCount,
			&item.SubscriberCovered, &item.ViewCovered, &item.VideoCovered,
		); err != nil {
			return stats.State{}, fmt.Errorf("scan channel stats evidence: %w", err)
		}
		digest, err := stats.SampleDigest(stats.Sample{
			SubscriberCount: item.SubscriberCount, ViewCount: item.ViewCount, VideoCount: item.VideoCount,
			SubscriberCovered: item.SubscriberCovered, ViewCovered: item.ViewCovered, VideoCovered: item.VideoCovered,
		})
		if err != nil {
			return stats.State{}, err
		}
		item.Digest = digest
		state.Slot = append(state.Slot, item)
	}
	return state, rows.Err()
}

func persistStatsDecision(ctx context.Context, tx dbx.Tx, observation Observation, decision stats.Decision) error {
	if err := persistStatsEvidence(ctx, tx, observation, decision); err != nil {
		return err
	}
	if err := persistStatsHead(ctx, tx, decision); err != nil {
		return err
	}
	if err := persistStatsSnapshots(ctx, tx, decision); err != nil {
		return err
	}
	return persistStatsConflict(ctx, tx, observation, decision)
}

func persistStatsEvidence(ctx context.Context, tx dbx.Tx, observation Observation, decision stats.Decision) error {
	if decision.Sample == nil {
		return nil
	}
	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_stats_evidence_upsert_0063_63.sql"),
		decision.Sample.ChannelID,
		decision.Sample.ScheduledFor,
		observation.Provider,
		observation.ID,
		decision.Sample.SubscriberCount,
		decision.Sample.ViewCount,
		decision.Sample.VideoCount,
		decision.Sample.SubscriberCovered,
		decision.Sample.ViewCovered,
		decision.Sample.VideoCovered,
		decision.Sample.EffectiveAt,
		decision.Sample.ReceivedAt,
	); err != nil {
		return fmt.Errorf("upsert channel stats evidence: %w", err)
	}
	return nil
}

func persistStatsHead(ctx context.Context, tx dbx.Tx, decision stats.Decision) error {
	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_stats_head_upsert_0066_66.sql"),
		decision.Head.ChannelID,
		decision.Head.LastResolvedScheduledFor,
		decision.Head.LastResolvedSubscriberCount,
		decision.Head.LastResolvedViewCount,
		decision.Head.LastResolvedVideoCount,
		decision.Head.PriorResolvedScheduledFor,
		decision.Head.PriorResolvedSubscriberCount,
		decision.Head.PriorResolvedViewCount,
		decision.Head.PriorResolvedVideoCount,
		decision.Head.UnresolvedScheduledFor,
	); err != nil {
		return fmt.Errorf("upsert channel stats head: %w", err)
	}
	return nil
}

func persistStatsSnapshots(ctx context.Context, tx dbx.Tx, decision stats.Decision) error {
	if decision.Sample == nil {
		return nil
	}
	if decision.ClearSnapshot {
		if _, err := tx.Exec(
			ctx,
			mustSQL("repository_stats_snapshot_delete_0068_68.sql"),
			decision.Sample.ChannelID,
			decision.Sample.ScheduledFor,
		); err != nil {
			return fmt.Errorf("clear unresolved channel stats snapshot: %w", err)
		}
	}
	if !decision.WriteSnapshot {
		return nil
	}
	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_stats_snapshot_insert_0067_67.sql"),
		decision.Sample.ChannelID,
		decision.Sample.ScheduledFor,
		decision.Sample.SubscriberCount,
		decision.Sample.ViewCount,
		decision.Sample.VideoCount,
	); err != nil {
		return fmt.Errorf("insert channel stats snapshot: %w", err)
	}
	return nil
}

func persistStatsConflict(ctx context.Context, tx dbx.Tx, observation Observation, decision stats.Decision) error {
	if decision.Conflict == nil || decision.Sample == nil {
		return nil
	}
	if err := persistReconcileConflict(ctx, tx, observation, "youtube_channel_stats", decision.Sample.ChannelID, decision.Conflict.FieldName, decision.Conflict.ExistingValueSHA256, decision.Conflict.AttemptedValueSHA256, "UNRESOLVED"); err != nil {
		return fmt.Errorf("insert channel stats reconciliation conflict: %w", err)
	}
	return nil
}
