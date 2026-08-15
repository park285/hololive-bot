package sourceobservation

import (
	"context"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	"github.com/kapu/hololive-shared/pkg/service/youtube/reconcile/content"
)

func persistContentDecision(
	ctx context.Context,
	tx dbx.Tx,
	writer CanonicalWriter,
	observation *Observation,
	decision *content.Decision,
) error {
	if writer == nil {
		return fmt.Errorf("persist content decision: canonical writer is not configured")
	}
	videos, notifications, tracking := contentArtifacts(observation.EffectiveAt, decision)
	if err := writer.PersistVideosTx(ctx, tx, videos, notifications, tracking, decision.Watermark); err != nil {
		return err
	}
	if err := persistContentFieldUpdates(ctx, tx, decision.FieldUpdates, observation.EffectiveAt); err != nil {
		return err
	}
	if err := persistContentClocks(ctx, tx, observation.ObservationKind, decision.Clocks); err != nil {
		return err
	}
	if err := persistContentAbsence(ctx, tx, observation, decision.AbsenceSlot); err != nil {
		return err
	}
	if err := persistContentHead(ctx, tx, observation, decision.EarliestCompleteAt); err != nil {
		return err
	}
	return persistContentConflicts(ctx, tx, observation, decision.Conflicts)
}

func contentArtifacts(
	effectiveAt time.Time,
	decision *content.Decision,
) ([]*domain.YouTubeVideo, []*domain.YouTubeNotificationOutbox, []*domain.YouTubeContentAlarmTracking) {
	videos := make([]*domain.YouTubeVideo, 0, len(decision.Videos))
	for i := range decision.Videos {
		videos = append(videos, domainVideo(decision.Videos[i], effectiveAt))
	}
	notifications := make([]*domain.YouTubeNotificationOutbox, 0, len(decision.Notifications))
	for i := range decision.Notifications {
		notifications = append(notifications, domainNotification(&decision.Notifications[i]))
	}
	tracking := make([]*domain.YouTubeContentAlarmTracking, 0, len(decision.Tracking))
	for i := range decision.Tracking {
		tracking = append(tracking, domainTracking(&decision.Tracking[i], effectiveAt))
	}
	return videos, notifications, tracking
}

func domainVideo(entity content.Entity, seenAt time.Time) *domain.YouTubeVideo {
	return &domain.YouTubeVideo{
		VideoID:     entity.VideoID,
		ChannelID:   entity.ChannelID,
		Title:       boundedVideoTitle(entity.Title),
		PublishedAt: entity.PublishedAt,
		IsShort:     entity.IsShort,
		FirstSeenAt: seenAt,
		LastSeenAt:  seenAt,
	}
}

func domainNotification(intent *content.NotificationIntent) *domain.YouTubeNotificationOutbox {
	video := domainVideo(intent.Video, time.Time{})
	payload := polling.MustMarshalJSON(video)
	if intent.Kind == domain.OutboxKindNewShort {
		payload = polling.BuildShortNotificationPayload(video, intent.ContentID)
	}
	return &domain.YouTubeNotificationOutbox{
		Kind:      intent.Kind,
		ChannelID: intent.ChannelID,
		ContentID: intent.ContentID,
		Payload:   payload,
		Status:    domain.OutboxStatusPending,
	}
}

func domainTracking(intent *content.NotificationIntent, detectedAt time.Time) *domain.YouTubeContentAlarmTracking {
	return &domain.YouTubeContentAlarmTracking{
		Kind:              intent.Kind,
		ContentID:         intent.ContentID,
		ChannelID:         intent.ChannelID,
		ActualPublishedAt: intent.Video.PublishedAt,
		DetectedAt:        detectedAt,
	}
}

func persistContentFieldUpdates(ctx context.Context, tx dbx.Tx, updates []content.Entity, seenAt time.Time) error {
	for i := range updates {
		if _, err := tx.Exec(
			ctx,
			mustSQL("repository_content_video_fields_0043_43.sql"),
			updates[i].VideoID,
			boundedVideoTitle(updates[i].Title),
			updates[i].PublishedAt,
			seenAt,
		); err != nil {
			return fmt.Errorf("update content video fields: %w", err)
		}
	}
	return nil
}

func persistContentClocks(ctx context.Context, tx dbx.Tx, kind contract.ObservationKind, clocks []content.EntityState) error {
	for i := range clocks {
		if clocks[i].LastPositiveValueSHA256 == "" {
			continue
		}
		if err := upsertContentClock(ctx, tx, kind, &clocks[i]); err != nil {
			return err
		}
	}
	return nil
}

func persistContentAbsence(ctx context.Context, tx dbx.Tx, observation *Observation, slot *content.AbsenceSlot) error {
	if slot == nil {
		return nil
	}
	coverage, err := content.MarshalCoverage(slot.Coverage)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_content_absence_upsert_0039_39.sql"),
		observation.SubjectKey,
		observation.ObservationKind,
		slot.ScheduledFor,
		observation.ID,
		slot.EvidenceSHA256,
		slot.EffectiveAt,
		slot.ReceivedAt,
		slot.ScopeSHA256,
		coverage,
	); err != nil {
		return fmt.Errorf("upsert content absence slot: %w", err)
	}
	return nil
}

func persistContentHead(ctx context.Context, tx dbx.Tx, observation *Observation, earliest *time.Time) error {
	if _, err := tx.Exec(
		ctx,
		mustSQL("repository_content_channel_head_upsert_0041_41.sql"),
		observation.SubjectKey,
		observation.ObservationKind,
		earliest,
	); err != nil {
		return fmt.Errorf("upsert content channel head: %w", err)
	}
	return nil
}

func persistContentConflicts(ctx context.Context, tx dbx.Tx, observation *Observation, conflicts []content.Conflict) error {
	for i := range conflicts {
		if _, err := tx.Exec(
			ctx,
			mustSQL("repository_content_conflict_insert_0042_42.sql"),
			observation.ID,
			observation.Provider,
			observation.ObservationKind,
			observation.SubjectKey,
			observation.ObservationKey,
			observation.EvidenceSHA256,
			conflicts[i].VideoID,
			conflicts[i].FieldName,
			observation.EffectiveAt,
			conflicts[i].ExistingValueSHA256,
			conflicts[i].AttemptedValueSHA256,
		); err != nil {
			return fmt.Errorf("insert content reconciliation conflict: %w", err)
		}
	}
	return nil
}

func boundedVideoTitle(title string) string {
	if len(title) > 500 {
		return title[:500]
	}
	return title
}
