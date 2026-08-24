package sourceobservation

import (
	"context"
	"errors"
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
		var (
			kind string
			item photo.Canonical
		)

		if err := rows.Scan(
			&kind, &item.Identity, &item.URL, &item.Width, &item.Height, &item.EffectiveAt,
			&item.Candidate, &item.CandidateURL, &item.CandidateW, &item.CandidateH,
			&item.Slots, &item.FirstAt, &item.LastAt, &item.FirstRx,
		); err != nil {
			return photo.State{}, fmt.Errorf("scan channel photo head: %w", err)
		}

		state.Head.Kinds[kind] = item
	}

	if err := rows.Err(); err != nil {
		return state, fmt.Errorf("load channel photo heads: %w", err)
	}

	return state, nil
}

func persistPhotoDecision(ctx context.Context, tx dbx.Tx, observation *Observation, decision *photo.Decision) error {
	if err := persistPhotoVariants(ctx, tx, observation, decision); err != nil {
		return fmt.Errorf("persist photo variants: %w", err)
	}

	if err := persistPhotoHeads(ctx, tx, decision); err != nil {
		return fmt.Errorf("persist photo heads: %w", err)
	}

	if err := persistPhotoProduct(ctx, tx, decision); err != nil {
		return fmt.Errorf("persist photo product: %w", err)
	}

	if err := persistPhotoConflicts(ctx, tx, observation, decision); err != nil {
		return fmt.Errorf("persist photo conflicts: %w", err)
	}

	return nil
}

func persistPhotoVariants(ctx context.Context, tx dbx.Tx, observation *Observation, decision *photo.Decision) error {
	if decision.Sample == nil {
		return nil
	}

	for i := range decision.Sample.Variants {
		if err := persistPhotoVariant(ctx, tx, observation, decision, &decision.Sample.Variants[i]); err != nil {
			return fmt.Errorf("persist photo variant: %w", err)
		}
	}

	return nil
}

func persistPhotoVariant(ctx context.Context, tx dbx.Tx, observation *Observation, decision *photo.Decision, variant *photo.Variant) error {
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

	return nil
}

func persistPhotoHeads(ctx context.Context, tx dbx.Tx, decision *photo.Decision) error {
	for kind := range decision.Head.Kinds {
		canonical := decision.Head.Kinds[kind]
		if canonical.Identity == "" && canonical.Candidate == "" {
			continue
		}

		if err := persistPhotoHead(ctx, tx, decision.Head.ChannelID, kind, &canonical); err != nil {
			return fmt.Errorf("persist photo head: %w", err)
		}
	}

	return nil
}

func persistPhotoHead(ctx context.Context, tx dbx.Tx, channelID, kind string, canonical *photo.Canonical) error {
	if canonical == nil {
		return errors.New("upsert channel photo head: canonical state is nil")
	}

	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_photo_head_upsert_0074_74.sql"),
		channelID,
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

	return nil
}

func persistPhotoProduct(ctx context.Context, tx dbx.Tx, decision *photo.Decision) error {
	if len(decision.WriteProduct) == 0 || decision.Sample == nil {
		return nil
	}

	avatar, banner := photoProductThumbnails(decision.WriteProduct)
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

	return nil
}

func photoProductThumbnails(product map[string]photo.Canonical) (avatarThumbnails, bannerThumbnails domain.ThumbnailsJSON) {
	var avatar, banner domain.ThumbnailsJSON

	if item, ok := product["avatar"]; ok {
		avatar = domain.ThumbnailsJSON{{URL: item.URL, Width: item.Width, Height: item.Height}}
	}

	if item, ok := product["banner"]; ok {
		banner = domain.ThumbnailsJSON{{URL: item.URL, Width: item.Width, Height: item.Height}}
	}

	return avatar, banner
}

func persistPhotoConflicts(ctx context.Context, tx dbx.Tx, observation *Observation, decision *photo.Decision) error {
	for i := range decision.Conflicts {
		if err := persistReconcileConflict(ctx, tx, observation, "youtube_channel_photo", observation.SubjectKey, decision.Conflicts[i].FieldName, decision.Conflicts[i].ExistingValueSHA256, decision.Conflicts[i].AttemptedValueSHA256, "KEEP_EXISTING"); err != nil {
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
