package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"slices"
	"time"

	"github.com/kapu/hololive-shared/internal/service/youtube/reconcile/live"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

func loadLivePendingEnds(ctx context.Context, tx dbx.Tx, state *live.State, videoIDs []string) error {
	if len(videoIDs) == 0 {
		return nil
	}

	rows, err := tx.Query(ctx, mustSQL("repository_live_pending_ends.sql"), videoIDs)
	if err != nil {
		return fmt.Errorf("query pending ends: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var pending live.PendingEnd

		if err := rows.Scan(&pending.VideoID, &pending.ChannelID, &pending.Kind, &pending.ObservationID,
			&pending.EffectiveAt, &pending.ReceivedAt, &pending.ScheduledFor, &pending.EndedAt,
			&pending.NegativeEligible, &pending.ScopeCovers); err != nil {
			return fmt.Errorf("scan pending end: %w", err)
		}

		state.PendingEnds[pending.VideoID] = pending
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate pending ends: %w", err)
	}

	return nil
}

// 이미 확인된 LIVE의 absence 효과는 session/head에 저장되어 있다. 신규 positive와 첫 LIVE
// 확인에만 과거 slot을 재적용하며 finalizer는 pending 사실만 사용한다.
func loadLiveAbsencesForPositive(ctx context.Context, tx dbx.Tx, state *live.State, evidence *live.Evidence) error {
	var (
		channels              []string
		includeBeforePositive bool
	)

	for i := range evidence.Sessions {
		fact := &evidence.Sessions[i]
		existing, present := state.Sessions[fact.VideoID]

		if existing.Status == live.StatusEnded {
			continue
		}

		if (fact.Status == "UPCOMING" && !present) ||
			(fact.Status == "LIVE" && existing.Clock.LastLivePositiveAt == nil) {
			channels = append(channels, fact.ChannelID)
			if fact.Status == "UPCOMING" || !fact.LiveStartConfirmed {
				includeBeforePositive = true
			}
		}
	}

	slices.Sort(channels)

	channels = slices.Compact(channels)

	after := &evidence.EffectiveAt

	if includeBeforePositive {
		after = nil
	}

	return loadLiveAbsenceSlots(ctx, tx, state, channels, after, evidence.ScheduledFor, evidence.Coverage.RequestedChannelIDs)
}

func loadLiveAbsenceSlots(ctx context.Context, tx dbx.Tx, state *live.State, channelIDs []string, after *time.Time,
	scheduledFor time.Time, currentChannels []string,
) error {
	if len(channelIDs) == 0 && len(currentChannels) == 0 {
		return nil
	}

	// 현재 slot은 이미 처리된 coverage가 정본이다. history를 생략해도 같은 slot을 새로 세면 안 된다.
	rows, err := tx.Query(ctx, mustSQL("repository_live_absence_slots.sql"), channelIDs, after, scheduledFor, currentChannels)
	if err != nil {
		return fmt.Errorf("query absence slots: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			slot     live.AbsenceSlot
			coverage []byte
		)

		if err := rows.Scan(&slot.ObservationID, &slot.ScheduledFor, &slot.EvidenceSHA256,
			&slot.EffectiveAt, &slot.ReceivedAt, &slot.ScopeSHA256, &coverage); err != nil {
			return fmt.Errorf("scan absence slot: %w", err)
		}

		if err := jsonv2.Unmarshal(coverage, &slot.Coverage); err != nil {
			return fmt.Errorf("decode absence coverage: %w", err)
		}

		state.AbsenceSlots = append(state.AbsenceSlots, slot)
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate absence slots: %w", err)
	}

	return nil
}

func persistLiveEvidence(ctx context.Context, tx dbx.Tx, decision *live.Decision) error {
	sessionIDs := make([]string, 0, len(decision.Sessions))
	for i := range decision.Sessions {
		sessionIDs = append(sessionIDs, decision.Sessions[i].VideoID)
	}

	pendingIDs := make([]string, 0, len(decision.PendingEnds))
	for i := range decision.PendingEnds {
		pending := &decision.PendingEnds[i]

		pendingIDs = append(pendingIDs, pending.VideoID)

		if _, err := tx.Exec(ctx, mustSQL("repository_live_pending_end_upsert.sql"),
			pending.VideoID, pending.ChannelID, pending.Kind, pending.ObservationID, pending.EffectiveAt,
			pending.ReceivedAt, pending.ScheduledFor, pending.EndedAt, pending.NegativeEligible, pending.ScopeCovers); err != nil {
			return fmt.Errorf("persist pending end: %w", err)
		}
	}

	// Decision에 없는 전역 pending을 지우면 다른 채널의 아직 도착하지 않은 positive를 잃는다.
	if _, err := tx.Exec(ctx, mustSQL("repository_live_pending_ends_delete.sql"), sessionIDs, pendingIDs); err != nil {
		return fmt.Errorf("delete settled pending ends: %w", err)
	}

	if decision.AbsenceSlot == nil {
		return nil
	}

	slot := decision.AbsenceSlot

	coverage, err := jsonv2.Marshal(slot.Coverage)
	if err != nil {
		return fmt.Errorf("encode absence coverage: %w", err)
	}

	if _, err := tx.Exec(ctx, mustSQL("repository_live_absence_slot_insert.sql"),
		slot.ObservationID, slot.ScheduledFor, slot.EvidenceSHA256, slot.EffectiveAt,
		slot.ReceivedAt, slot.ScopeSHA256, string(coverage)); err != nil {
		return fmt.Errorf("persist absence slot: %w", err)
	}

	return nil
}
