package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/content"
	"github.com/kapu/hololive-shared/pkg/contracts/youtubeoutbox"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

func persistContentDecision(
	ctx context.Context,
	tx dbx.Tx,
	writer canonicalWriter,
	observation *Observation,
	loaded *content.State,
	decision *content.Decision,
) error {
	videos, notifications, tracking := contentArtifacts(observation.EffectiveAt, decision)
	if err := writer.PersistVideosTx(ctx, tx, videos, notifications, tracking, decision.Watermark); err != nil {
		return fmt.Errorf("persist videos tx: %w", err)
	}

	if err := persistContentFieldUpdates(ctx, tx, decision.FieldUpdates, observation.EffectiveAt); err != nil {
		return fmt.Errorf("persist content field updates: %w", err)
	}

	if err := persistContentClocks(ctx, tx, loaded.Videos, decision.Clocks); err != nil {
		return fmt.Errorf("persist content clocks: %w", err)
	}

	if err := persistContentAbsence(ctx, tx, observation, decision.AbsenceSlot); err != nil {
		return fmt.Errorf("persist content absence: %w", err)
	}

	if err := persistContentHead(ctx, tx, observation, decision.EarliestCompleteAt); err != nil {
		return fmt.Errorf("persist content head: %w", err)
	}

	if err := persistContentConflicts(ctx, tx, observation, decision.Conflicts); err != nil {
		return fmt.Errorf("persist content conflicts: %w", err)
	}

	return nil
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

	var payload string

	if intent.Kind == domain.OutboxKindNewShort {
		payload = mustMarshalPayload(youtubeoutbox.Short{
			VideoFields:     youtubeoutbox.NewVideoFields(video),
			CanonicalPostID: shortCanonicalPostID(intent.ContentID),
		})
	} else {
		payload = mustMarshalPayload(youtubeoutbox.Video{
			VideoFields:      youtubeoutbox.NewVideoFields(video),
			ScheduledStartAt: intent.Video.ScheduledFor,
			IsPremiere:       intent.Video.IsPremiere,
		})
	}

	return &domain.YouTubeNotificationOutbox{
		Kind:      intent.Kind,
		ChannelID: intent.ChannelID,
		ContentID: intent.ContentID,
		Payload:   payload,
		Status:    domain.OutboxStatusPending,
	}
}

// shortCanonicalPostID는 쇼츠 payload의 canonical_post_id를 만든다. 정규화할 수 없는 값은 빈 문자열로 싣고
// canonical 저장 전 검증이 그 행을 거부한다.
func shortCanonicalPostID(contentID string) string {
	canonicalID, err := ytcontentid.ForShort(contentID)
	if err != nil {
		return ""
	}

	return canonicalID
}

// mustMarshalPayload는 outbox payload 계약 값을 JSON 문자열로 만든다. 계약 타입은 항상 직렬화할 수 있어야 하므로
// 실패는 프로그래밍 오류로 보고 panic한다.
func mustMarshalPayload(v any) string {
	data, err := jsonv2.Marshal(v)
	if err != nil {
		panic(err)
	}

	return string(data)
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
	statements := make([]dbx.Statement, 0, len(updates))

	for i := range updates {
		statements = append(statements, dbx.Statement{
			Operation: "update content video fields",
			SQL:       mustSQL("repository_content_video_fields_0043_43.sql"),
			Args: []any{
				updates[i].VideoID,
				boundedVideoTitle(updates[i].Title),
				updates[i].PublishedAt,
				seenAt,
			},
		})
	}

	if err := dbx.ExecStatements(ctx, tx, statements); err != nil {
		return fmt.Errorf("update content video fields: %w", err)
	}

	return nil
}

func persistContentAbsence(ctx context.Context, tx dbx.Tx, observation *Observation, slot *content.AbsenceSlot) error {
	if slot == nil {
		return nil
	}

	coverage, err := content.MarshalCoverage(slot.Coverage)
	if err != nil {
		return fmt.Errorf("marshal coverage: %w", err)
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
		if err := persistReconcileConflict(
			ctx,
			tx,
			observation,
			"youtube_video",
			conflicts[i].VideoID,
			conflicts[i].FieldName,
			conflicts[i].ExistingValueSHA256,
			conflicts[i].AttemptedValueSHA256,
			"KEEP_EXISTING",
		); err != nil {
			return fmt.Errorf("insert content reconciliation conflict: %w", err)
		}
	}

	return nil
}

func boundedVideoTitle(title string) string {
	const maxTitleBytes = 500

	if len(title) <= maxTitleBytes {
		return title
	}

	// 기존 바이트 상한을 지키면서 JSON과 PostgreSQL에 유효한 UTF-8 접두사만 저장한다.
	end := maxTitleBytes
	for end > 0 && !utf8.RuneStart(title[end]) {
		end--
	}

	return title[:end]
}
