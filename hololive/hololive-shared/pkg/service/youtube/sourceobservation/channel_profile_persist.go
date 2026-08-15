package sourceobservation

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/profile"
)

func loadProfileState(ctx context.Context, tx dbx.Tx, channelID string) (profile.State, error) {
	state := profile.State{ChannelID: channelID, Head: profile.Head{ChannelID: channelID}}
	err := tx.QueryRow(ctx, mustSQL("repository_profile_head_0070_70.sql"), channelID).Scan(
		&state.Head.ChannelID,
		&state.Head.Handle.Set, &state.Head.Handle.Value, &state.Head.Handle.EffectiveAt,
		&state.Head.Description.Set, &state.Head.Description.Value, &state.Head.Description.EffectiveAt,
		&state.Head.Description.EmptySlots, &state.Head.Description.EmptyFirstAt,
		&state.Head.Description.EmptyLastAt, &state.Head.Description.EmptyFirstRx,
		&state.Head.Country.Set, &state.Head.Country.Value, &state.Head.Country.EffectiveAt,
		&state.Head.Country.EmptySlots, &state.Head.Country.EmptyFirstAt,
		&state.Head.Country.EmptyLastAt, &state.Head.Country.EmptyFirstRx,
		&state.Head.JoinedDate.Set, &state.Head.JoinedDate.Value, &state.Head.JoinedDate.EffectiveAt,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return profile.State{}, fmt.Errorf("load channel profile head: %w", err)
	}
	return state, nil
}

func persistProfileDecision(ctx context.Context, tx dbx.Tx, observation Observation, decision profile.Decision) error {
	if err := persistProfileEvidence(ctx, tx, observation, decision); err != nil {
		return err
	}
	if err := persistProfileHead(ctx, tx, decision); err != nil {
		return err
	}
	return persistProfileConflicts(ctx, tx, observation, decision)
}

func persistProfileEvidence(ctx context.Context, tx dbx.Tx, observation Observation, decision profile.Decision) error {
	if decision.Sample == nil {
		return nil
	}
	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_profile_evidence_upsert_0069_69.sql"),
		decision.Sample.ChannelID,
		decision.Sample.ScheduledFor,
		observation.Provider,
		observation.ID,
		decision.Sample.Handle.Present,
		decision.Sample.Handle.Value,
		decision.Sample.Description.Present,
		decision.Sample.Description.Value,
		decision.Sample.Country.Present,
		decision.Sample.Country.Value,
		decision.Sample.JoinedDate.Present,
		decision.Sample.JoinedDate.Value,
		decision.Sample.Complete,
		decision.Sample.EffectiveAt,
		decision.Sample.ReceivedAt,
	); err != nil {
		return fmt.Errorf("upsert channel profile evidence: %w", err)
	}
	return nil
}

func persistProfileHead(ctx context.Context, tx dbx.Tx, decision profile.Decision) error {
	if !decision.WriteHead {
		return nil
	}
	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_profile_head_upsert_0071_71.sql"),
		decision.Head.ChannelID,
		decision.Head.Handle.Set, decision.Head.Handle.Value, decision.Head.Handle.EffectiveAt,
		decision.Head.Description.Set, decision.Head.Description.Value, decision.Head.Description.EffectiveAt,
		decision.Head.Description.EmptySlots, decision.Head.Description.EmptyFirstAt,
		decision.Head.Description.EmptyLastAt, decision.Head.Description.EmptyFirstRx,
		decision.Head.Country.Set, decision.Head.Country.Value, decision.Head.Country.EffectiveAt,
		decision.Head.Country.EmptySlots, decision.Head.Country.EmptyFirstAt,
		decision.Head.Country.EmptyLastAt, decision.Head.Country.EmptyFirstRx,
		decision.Head.JoinedDate.Set, decision.Head.JoinedDate.Value, decision.Head.JoinedDate.EffectiveAt,
	); err != nil {
		return fmt.Errorf("upsert channel profile head: %w", err)
	}
	return nil
}

func persistProfileConflicts(ctx context.Context, tx dbx.Tx, observation Observation, decision profile.Decision) error {
	for i := range decision.Conflicts {
		conflict := decision.Conflicts[i]
		if err := persistReconcileConflict(ctx, tx, observation, "youtube_channel_profile", observation.SubjectKey, conflict.FieldName, conflict.ExistingValueSHA256, conflict.AttemptedValueSHA256, "KEEP_EXISTING"); err != nil {
			return fmt.Errorf("insert channel profile reconciliation conflict: %w", err)
		}
	}
	return nil
}
