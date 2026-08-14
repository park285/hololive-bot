package live

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func emptyState() State {
	return State{Sessions: map[string]SessionState{}, PendingEnds: map[string]PendingEnd{}}
}

func channelCoverage() contract.GlobalChannelCoverageV1 {
	return contract.GlobalChannelCoverageV1{
		RequestedChannelIDs: []string{"UC_TEST"},
		Filters:             contract.LiveFiltersV1{Statuses: []string{"UPCOMING", "LIVE", "ENDED", "CANCELLED"}},
	}
}

func sessionFact(videoID, status string) SessionFact {
	return SessionFact{VideoID: videoID, ChannelID: "UC_TEST", Status: status}
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
	return liveEvidence(1, time.Date(2026, 8, 14, 1, 0, 0, 0, time.UTC), contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact("vid-a", "UPCOMING"))
}

func liveA() Evidence {
	return liveEvidence(2, time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC), contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact("vid-a", "LIVE"))
}

func endA() Evidence {
	return liveEvidence(3, time.Date(2026, 8, 14, 3, 0, 0, 0, time.UTC), contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact("vid-a", "ENDED"))
}

func cancelA() Evidence {
	return liveEvidence(8, time.Date(2026, 8, 14, 3, 0, 0, 0, time.UTC), contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact("vid-a", "CANCELLED"))
}

func absenceAt(id int64, at time.Time) Evidence {
	return liveEvidence(id, at, contract.CompletenessComplete, contract.ContinuityContiguous)
}

func firstAbsence() Evidence {
	return absenceAt(4, time.Date(2026, 8, 14, 3, 0, 0, 0, time.UTC))
}

func secondAbsence(grace time.Duration) Evidence {
	at := time.Date(2026, 8, 14, 4, 0, 0, 0, time.UTC)
	evidence := absenceAt(5, at)
	evidence.ReceivedAt = at.Add(grace)
	return evidence
}

func partialAbsence() Evidence {
	return liveEvidence(6, time.Date(2026, 8, 14, 3, 0, 0, 0, time.UTC), contract.CompletenessPartial, contract.ContinuityContiguous)
}

func gapAbsence() Evidence {
	return liveEvidence(7, time.Date(2026, 8, 14, 3, 0, 0, 0, time.UTC), contract.CompletenessComplete, contract.ContinuityGapUnresolved)
}

func lateLiveA() Evidence {
	return liveEvidence(9, time.Date(2026, 8, 14, 5, 0, 0, 0, time.UTC), contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact("vid-a", "LIVE"))
}

func sameTimeLiveA() Evidence {
	return liveEvidence(10, time.Date(2026, 8, 14, 3, 0, 0, 0, time.UTC), contract.CompletenessComplete, contract.ContinuityContiguous, sessionFact("vid-a", "LIVE"))
}

func mustReduceAll(t *testing.T, state State, evidence []Evidence, grace time.Duration) Decision {
	t.Helper()
	current := state
	var decision Decision
	latest := time.Time{}
	for i := range evidence {
		next, err := Reduce(current, evidence[i], grace, evidence[i].ReceivedAt)
		if err != nil {
			t.Fatalf("reduce[%d]: %v", i, err)
		}
		decision = next
		current = stateFromDecision(current, next, evidence[i])
		if evidence[i].ReceivedAt.After(latest) {
			latest = evidence[i].ReceivedAt
		}
	}
	if !latest.IsZero() {
		decision = FinalizeDue(current, latest, grace)
		current = stateFromDecision(current, decision, Evidence{})
	}
	return decisionFromState(current)
}

func decisionFromState(state State) Decision {
	sessions := make([]SessionState, 0, len(state.Sessions))
	for _, session := range state.Sessions {
		sessions = append(sessions, session)
	}
	pending := make([]PendingEnd, 0, len(state.PendingEnds))
	for _, fact := range state.PendingEnds {
		pending = append(pending, fact)
	}
	return Decision{Sessions: sessions, PendingEnds: pending}
}

func stateFromDecision(previous State, decision Decision, evidence Evidence) State {
	next := previous.clone()
	if next.Sessions == nil {
		next.Sessions = map[string]SessionState{}
	}
	if next.PendingEnds == nil {
		next.PendingEnds = map[string]PendingEnd{}
	}
	for _, session := range decision.Sessions {
		next.Sessions[session.VideoID] = session
	}
	next.PendingEnds = map[string]PendingEnd{}
	for _, pending := range decision.PendingEnds {
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
	_ = evidence
	return next
}

func sessionOf(decision Decision, videoID string) SessionState {
	for _, session := range decision.Sessions {
		if session.VideoID == videoID {
			return session
		}
	}
	return SessionState{}
}

func snapshotDecision(decision Decision) string {
	parts := make([]string, 0, len(decision.Sessions))
	for _, session := range decision.Sessions {
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
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

func assertAllPermutationsConverge(t *testing.T, state State, evidence []Evidence, grace time.Duration) {
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
