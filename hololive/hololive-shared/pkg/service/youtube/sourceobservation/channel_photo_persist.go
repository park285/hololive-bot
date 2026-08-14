package sourceobservation

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/photo"
)

func loadPhotoState(ctx context.Context, tx dbx.Tx, channelID string) (photo.State, error) {
	state := photo.State{ChannelID: channelID, Head: photo.Head{ChannelID: channelID, Kinds: map[string]photo.Canonical{}}}
	rows, err := tx.Query(ctx, mustSQL("repository_photo_head_0073_73.sql"), channelID)
	if err != nil {
		return photo.State{}, fmt.Errorf("load channel photo heads: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var item photo.Canonical
		if err := rows.Scan(
			&kind, &item.Identity, &item.URL, &item.Width, &item.Height, &item.EffectiveAt,
			&item.Candidate, &item.CandidateURL, &item.CandidateW, &item.CandidateH,
			&item.Slots, &item.FirstAt, &item.LastAt, &item.FirstRx,
		); err != nil {
			return photo.State{}, fmt.Errorf("scan channel photo head: %w", err)
		}
		state.Head.Kinds[kind] = item
	}
	return state, rows.Err()
}

func persistPhotoDecision(ctx context.Context, tx dbx.Tx, observation Observation, decision photo.Decision) error {
	if decision.Sample != nil {
		for i := range decision.Sample.Variants {
			variant := decision.Sample.Variants[i]
			if _, err := tx.Exec(
				ctx,
				mustSQL("repository_photo_variant_upsert_0072_72.sql"),
				decision.Sample.ChannelID,
				variant.Kind,
				observation.Provider,
				decision.Sample.ScheduledFor,
				variant.URL,
				variant.Width,
				variant.Height,
				variant.StableMediaID,
				variant.ContentFingerprint,
				observation.ID,
				decision.Sample.EffectiveAt,
				decision.Sample.ReceivedAt,
			); err != nil {
				return fmt.Errorf("upsert channel photo variant: %w", err)
			}
		}
	}
	for kind, canonical := range decision.Head.Kinds {
		if canonical.Identity == "" && canonical.Candidate == "" {
			continue
		}
		if _, err := tx.Exec(
			ctx,
			mustSQL("repository_photo_head_upsert_0074_74.sql"),
			decision.Head.ChannelID,
			kind,
			canonical.Identity,
			canonical.URL,
			canonical.Width,
			canonical.Height,
			canonical.EffectiveAt,
			canonical.Candidate,
			canonical.CandidateURL,
			canonical.CandidateW,
			canonical.CandidateH,
			canonical.Slots,
			canonical.FirstAt,
			canonical.LastAt,
			canonical.FirstRx,
		); err != nil {
			return fmt.Errorf("upsert channel photo head: %w", err)
		}
	}
	if len(decision.WriteProduct) > 0 && decision.Sample != nil {
		var avatar, banner domain.ThumbnailsJSON
		if item, ok := decision.WriteProduct["avatar"]; ok {
			avatar = domain.ThumbnailsJSON{{URL: item.URL, Width: item.Width, Height: item.Height}}
		}
		if item, ok := decision.WriteProduct["banner"]; ok {
			banner = domain.ThumbnailsJSON{{URL: item.URL, Width: item.Width, Height: item.Height}}
		}
		if _, err := tx.Exec(
			ctx,
			mustSQL("repository_photo_product_upsert_0075_75.sql"),
			decision.Sample.ChannelID,
			nullableThumbnails(avatar),
			nullableThumbnails(banner),
			decision.Sample.EffectiveAt,
		); err != nil {
			return fmt.Errorf("upsert channel photo product: %w", err)
		}
	}
	for i := range decision.Conflicts {
		conflict := decision.Conflicts[i]
		if _, err := tx.Exec(
			ctx,
			mustSQL("repository_reconcile_conflict_insert_0061_61.sql"),
			observation.ID,
			observation.Provider,
			observation.ObservationKind,
			observation.SubjectKey,
			observation.ObservationKey,
			observation.EvidenceSHA256,
			"youtube_channel_photo",
			observation.SubjectKey,
			conflict.FieldName,
			observation.EffectiveAt,
			conflict.ExistingValueSHA256,
			conflict.AttemptedValueSHA256,
			"KEEP_EXISTING",
		); err != nil {
			return fmt.Errorf("insert channel photo reconciliation conflict: %w", err)
		}
	}
	return nil
}

func nullableThumbnails(value domain.ThumbnailsJSON) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
