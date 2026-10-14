package sourceobservation

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/live"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
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

// 먼저 잠근 positive clock으로 과거 부재 replay 여부를 정한다. 일반 PARTIAL도 현재 slot의
// 저장된 coverage는 복원하되, 그 slot에서 보인 영상의 무시 이력은 읽지 않는다.
func loadLiveAbsencesForPositive(ctx context.Context, tx dbx.Tx, state *live.State, evidence *live.Evidence) error {
	var (
		channels              []string
		includeBeforePositive bool
	)

	for i := range evidence.Sessions {
		fact := &evidence.Sessions[i]
		existing, present := state.Sessions[fact.VideoID]

		if existing.Status == domain.LiveStatusEnded {
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

	if len(channels) == 0 && !scopedLiveNegative(evidence) {
		return loadLiveCurrentAbsences(ctx, tx, state, evidence)
	}

	if err := loadLiveIgnoredAbsences(ctx, tx, state, liveIgnoredAbsenceLoadIDs(state, evidence)); err != nil {
		return fmt.Errorf("load ignored absences: %w", err)
	}

	slices.Sort(channels)

	channels = slices.Compact(channels)

	after := &evidence.EffectiveAt

	if includeBeforePositive {
		after = nil
	}

	return loadLiveAbsenceSlots(ctx, tx, state, channels, after, evidence.ScheduledFor, evidence.Coverage.RequestedChannelIDs)
}

func loadLiveCurrentAbsences(ctx context.Context, tx dbx.Tx, state *live.State, evidence *live.Evidence) error {
	if err := loadLiveAbsenceSlots(ctx, tx, state, nil, nil, evidence.ScheduledFor, evidence.Coverage.RequestedChannelIDs); err != nil {
		return err
	}

	if len(state.AbsenceSlots) == 0 {
		return nil
	}

	// PARTIAL 자체는 부재를 만들지 않지만 다른 kind의 늦은 positive 뒤에는 저장된 slot이
	// 처음 적용될 수 있다. reducer가 현재 slot에서 건너뛰는 payload 영상만 이력을 생략한다.
	seen := make(map[string]struct{}, len(evidence.Sessions))
	for i := range evidence.Sessions {
		seen[evidence.Sessions[i].VideoID] = struct{}{}
	}

	ids := slices.DeleteFunc(liveIgnoredAbsenceLoadIDs(state, evidence), func(videoID string) bool {
		_, present := seen[videoID]
		return present
	})
	if err := loadLiveIgnoredAbsences(ctx, tx, state, ids); err != nil {
		return fmt.Errorf("load current slot ignored absences: %w", err)
	}

	return nil
}

func scopedLiveNegative(evidence *live.Evidence) bool {
	return contract.NegativeEligible(evidence.Completeness, evidence.Continuity) &&
		contract.AbsenceCapabilityFor(evidence.Kind) == contract.AbsenceScoped
}

func liveIgnoredAbsenceLoadIDs(state *live.State, evidence *live.Evidence) []string {
	var ids []string

	for videoID := range state.Sessions {
		session := state.Sessions[videoID]

		if !session.HeadPresent || session.Status == domain.LiveStatusEnded {
			continue
		}

		if _, loaded := session.IgnoredAbsences.Slots(); loaded {
			continue
		}

		if (session.LifecycleOrigin == live.OriginMetadataOnly || session.LifecycleOrigin == live.OriginLegacyUnknown) &&
			session.Clock.LastUpcomingPositiveAt == nil && session.Clock.LastLivePositiveAt == nil {
			// positive 적용 뒤에는 metadata도 observed가 되어 replay 대상이 된다.
			// 승격되는 영상의 기존 이력을 빈 배열로 간주하거나 생략하면 안 된다.
			promoted := slices.ContainsFunc(evidence.Sessions, func(fact live.SessionFact) bool {
				return fact.VideoID == videoID &&
					((fact.Status == "UPCOMING" && session.Status != domain.LiveStatusLive) ||
						(fact.Status == "LIVE" && fact.LiveStartConfirmed))
			})
			if !promoted {
				continue
			}
		}

		ids = append(ids, videoID)
	}

	return ids
}

func loadLiveAbsenceSlots(ctx context.Context, tx dbx.Tx, state *live.State, channelIDs []string, after *time.Time,
	scheduledFor time.Time, currentChannels []string,
) error {
	if len(channelIDs) == 0 && len(currentChannels) == 0 {
		return nil
	}

	// 현재 slot은 이미 처리된 coverage가 정본이다. history를 생략해도 같은 slot을 새로 세면 안 된다.
	var (
		rows pgx.Rows
		err  error
	)

	if len(channelIDs) == 0 {
		rows, err = tx.Query(ctx, mustSQL("repository_live_absence_current_slot.sql"), scheduledFor, currentChannels)
	} else {
		rows, err = tx.Query(ctx, mustSQL("repository_live_absence_slots.sql"), channelIDs, after, scheduledFor, currentChannels)
	}

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

	pendingIDs := make([]string, len(decision.PendingEnds))
	for i := range decision.PendingEnds {
		pendingIDs[i] = decision.PendingEnds[i].VideoID
	}

	if err := persistPendingLiveEnds(ctx, tx, decision.PendingEnds); err != nil {
		return fmt.Errorf("persist pending ends: %w", err)
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

const pendingLiveEndBatchSize = 128

func persistPendingLiveEnds(ctx context.Context, tx dbx.Tx, pending []live.PendingEnd) error {
	for start := 0; start < len(pending); start += pendingLiveEndBatchSize {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("persist pending live ends at %d: %w", start, err)
		}

		end := min(start+pendingLiveEndBatchSize, len(pending))
		statements := pendingLiveEndStatements(pending[start:end])

		if err := dbx.ExecStatements(ctx, tx, statements); err != nil {
			return fmt.Errorf("persist pending live end batch at %d: %w", start, err)
		}
	}

	return nil
}

func pendingLiveEndStatements(pending []live.PendingEnd) []dbx.Statement {
	statements := make([]dbx.Statement, 0, len(pending))
	for i := range pending {
		item := &pending[i]

		statements = append(statements, dbx.Statement{
			Operation: "upsert pending live end",
			SQL:       mustSQL("repository_live_pending_end_upsert.sql"),
			Args: []any{
				item.VideoID, item.ChannelID, item.Kind, item.ObservationID, item.EffectiveAt,
				item.ReceivedAt, item.ScheduledFor, item.EndedAt, item.NegativeEligible, item.ScopeCovers,
			},
		})
	}

	return statements
}
