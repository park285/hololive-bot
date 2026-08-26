package live

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func emptyState() *State {
	return &State{Sessions: map[string]SessionState{}, PendingEnds: map[string]PendingEnd{}}
}

func channelCoverage() contract.GlobalChannelCoverageV1 {
	return contract.GlobalChannelCoverageV1{
		RequestedChannelIDs: []string{"UC_TEST"},
		Filters:             contract.LiveFiltersV1{Statuses: []string{"UPCOMING", "LIVE", "ENDED", "CANCELLED"}}, //nolint:misspell // YouTube 방송 상태 계약값이 영국식 CANCELLED라, canceled로 바꾸면 상태 판정이 어긋난다.
	}
}

func sessionFact(status string) SessionFact {
	return SessionFact{VideoID: "vid-a", ChannelID: "UC_TEST", Status: status}
}

func liveEvidence(id int64, at time.Time, completeness contract.Completeness, continuity contract.Continuity, facts ...SessionFact) Evidence {
	return Evidence{
		Kind:           contract.KindLiveSnapshot,
		ObservationID:  id,
		ScheduledFor:   at,
		EffectiveAt:    at,
		ReceivedAt:     at,
		Completeness:   completeness,
		Continuity:     continuity,
		Sessions:       facts,
		Coverage:       channelCoverage(),
		ScopeSHA256:    strings.Repeat("ab", 32),
		EvidenceSHA256: fmt.Sprintf("%064d", id),
	}
}

func upcomingA() Evidence {
	return liveEvidence(1, time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC), contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact("UPCOMING"))
}

func liveA() Evidence {
	return liveEvidence(2, time.Date(2026, time.August, 14, 2, 0, 0, 0, time.UTC), contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact("LIVE"))
}

func endA() Evidence {
	return liveEvidence(3, time.Date(2026, time.August, 14, 3, 0, 0, 0, time.UTC), contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact("ENDED"))
}

func cancelA() Evidence {
	return liveEvidence(8, time.Date(2026, time.August, 14, 3, 0, 0, 0, time.UTC), contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact("CANCELLED")) //nolint:misspell // YouTube 방송 상태 계약값이 영국식 CANCELLED라, canceled로 바꾸면 상태 판정이 어긋난다.
}

func absenceAt(id int64, at time.Time) Evidence {
	return liveEvidence(id, at, contract.CompletenessComplete, contract.ContinuityContiguous)
}

func firstAbsence() Evidence {
	return absenceAt(4, time.Date(2026, time.August, 14, 3, 0, 0, 0, time.UTC))
}

func secondAbsence(grace time.Duration) Evidence {
	at := time.Date(2026, time.August, 14, 4, 0, 0, 0, time.UTC)
	evidence := absenceAt(5, at)

	evidence.ReceivedAt = at.Add(grace)

	return evidence
}

func partialAbsence() Evidence {
	return liveEvidence(6, time.Date(2026, time.August, 14, 3, 0, 0, 0, time.UTC), contract.CompletenessPartial, contract.ContinuityContiguous)
}

func gapAbsence() Evidence {
	return liveEvidence(7, time.Date(2026, time.August, 14, 3, 0, 0, 0, time.UTC), contract.CompletenessComplete, contract.ContinuityGapUnresolved)
}

func lateLiveA() Evidence {
	return liveEvidence(9, time.Date(2026, time.August, 14, 5, 0, 0, 0, time.UTC), contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact("LIVE"))
}

func sameTimeLiveA() Evidence {
	return liveEvidence(10, time.Date(2026, time.August, 14, 3, 0, 0, 0, time.UTC), contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact("LIVE"))
}

func mustReduceAll(t *testing.T, state *State, evidence []Evidence, grace time.Duration) *Decision {
	t.Helper()

	current := state.clone()
	latest := time.Time{}

	for i := range evidence {
		next, err := Reduce(current, evidence[i], grace, evidence[i].ReceivedAt)
		if err != nil {
			t.Fatalf("reduce[%d]: %v", i, err)
		}

		current = stateFromDecision(&current, &next)

		if evidence[i].ReceivedAt.After(latest) {
			latest = evidence[i].ReceivedAt
		}
	}

	if !latest.IsZero() {
		decision := FinalizeDue(current, latest, grace)

		current = stateFromDecision(&current, &decision)
	}

	return decisionFromState(&current)
}

func decisionFromState(state *State) *Decision {
	sessions := make([]SessionState, 0, len(state.Sessions))
	for videoID := range state.Sessions {
		session := state.Sessions[videoID]

		sessions = append(sessions, session)
	}

	pending := make([]PendingEnd, 0, len(state.PendingEnds))
	for videoID := range state.PendingEnds {
		fact := state.PendingEnds[videoID]

		pending = append(pending, fact)
	}

	return &Decision{Sessions: sessions, PendingEnds: pending}
}

func stateFromDecision(previous *State, decision *Decision) State {
	next := previous.clone()
	if next.Sessions == nil {
		next.Sessions = map[string]SessionState{}
	}

	if next.PendingEnds == nil {
		next.PendingEnds = map[string]PendingEnd{}
	}

	for i := range decision.Sessions {
		session := decision.Sessions[i]

		next.Sessions[session.VideoID] = session
	}

	next.PendingEnds = map[string]PendingEnd{}
	for i := range decision.PendingEnds {
		pending := decision.PendingEnds[i]

		next.PendingEnds[pending.VideoID] = pending
	}

	if decision.AbsenceSlot != nil {
		replaced := false

		for i := range next.AbsenceSlots {
			if next.AbsenceSlots[i].ScheduledFor.Equal(decision.AbsenceSlot.ScheduledFor) {
				next.AbsenceSlots[i] = *decision.AbsenceSlot
				replaced = true
			}
		}

		if !replaced {
			next.AbsenceSlots = append(next.AbsenceSlots, *decision.AbsenceSlot)
		}
	}

	return next
}

func sessionOf(decision *Decision) SessionState {
	for i := range decision.Sessions {
		if decision.Sessions[i].VideoID == "vid-a" {
			return decision.Sessions[i]
		}
	}

	return SessionState{}
}

func snapshotDecision(decision *Decision) string {
	parts := make([]string, 0, len(decision.Sessions))
	for i := range decision.Sessions {
		session := &decision.Sessions[i]
		reason := ""

		if session.EndReason != nil {
			reason = string(*session.EndReason)
		}

		candidate := ""

		if session.Clock.EndCandidateKind != nil {
			candidate = string(*session.Clock.EndCandidateKind)
		}

		parts = append(parts, fmt.Sprintf("%s|%s|ended=%t|slots=%d|cand=%s|reason=%s",
			session.VideoID, session.Status, session.Status == StatusEnded,
			session.Clock.ConsecutiveAbsenceSlots, candidate, reason))
	}

	slices.Sort(parts)

	return strings.Join(parts, ";")
}

func assertAllPermutationsConverge(t *testing.T, state *State, evidence []Evidence, grace time.Duration) {
	t.Helper()

	var want string

	for _, order := range permutations(evidence) {
		got := snapshotDecision(mustReduceAll(t, state, order, grace))

		if want == "" {
			want = got
			continue
		}

		if got != want {
			t.Fatalf("permutation %v diverged\n got %s\nwant %s", observationIDs(order), got, want)
		}
	}
}

func permutations(items []Evidence) [][]Evidence {
	if len(items) == 0 {
		return [][]Evidence{{}}
	}

	var result [][]Evidence

	for i := range items {
		rest := append([]Evidence{}, items[:i]...)

		rest = append(rest, items[i+1:]...)

		for _, perm := range permutations(rest) {
			result = append(result, append([]Evidence{items[i]}, perm...))
		}
	}

	return result
}

func observationIDs(items []Evidence) []int64 {
	ids := make([]int64, len(items))
	for i := range items {
		ids[i] = items[i].ObservationID
	}

	return ids
}
