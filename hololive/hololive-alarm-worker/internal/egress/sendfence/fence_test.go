package sendfence

import (
	"errors"
	"slices"
	"testing"
)

// matched는 조건이 모두 맞지만 적용되지 않은 관측이다. 각 시험은 필요한 조건만 거짓으로 바꾼다.
func matched(id int64) Observation {
	return Observation{
		ID: id, Status: "SENDING",
		StatusMatches: true, OwnerMatches: true, VersionMatches: true, LeaseValid: true,
		MembershipMatches: true, MembersNonTerminal: true, RequestMatches: true,
	}
}

func TestCheckReturnsNilWhenEveryTargetApplied(t *testing.T) {
	first, second := matched(1), matched(2)

	first.Applied, second.Applied = true, true

	if err := Check("mark_sent", []int64{1, 2}, []Observation{second, first}); err != nil {
		t.Fatalf("Check() = %v, want nil", err)
	}
}

func TestCheckClassifiesFirstFailedConditionInOrder(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Observation)
		want   Reason
	}{
		{"status dominates every later condition", func(o *Observation) {
			o.StatusMatches, o.OwnerMatches, o.VersionMatches, o.LeaseValid = false, false, false, false
			o.MembersNonTerminal, o.MembershipMatches, o.RequestMatches = false, false, false
		}, ReasonStatusChanged},
		{"owner dominates version", func(o *Observation) {
			o.OwnerMatches, o.VersionMatches, o.LeaseValid, o.RequestMatches = false, false, false, false
		}, ReasonOwnerChanged},
		{"version dominates lease", func(o *Observation) {
			o.VersionMatches, o.LeaseValid, o.MembershipMatches = false, false, false
		}, ReasonVersionChanged},
		{"lease dominates membership", func(o *Observation) {
			o.LeaseValid, o.MembersNonTerminal, o.MembershipMatches = false, false, false
		}, ReasonLeaseExpired},
		{"terminal member dominates membership change", func(o *Observation) {
			o.MembersNonTerminal, o.MembershipMatches, o.RequestMatches = false, false, false
		}, ReasonMemberTerminal},
		{"membership dominates request", func(o *Observation) {
			o.MembershipMatches, o.RequestMatches = false, false
		}, ReasonMembershipChanged},
		{"request", func(o *Observation) { o.RequestMatches = false }, ReasonRequestChanged},
		{"all conditions held but a peer missed", func(*Observation) {}, ReasonPeerMissed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observation := matched(7)
			tc.mutate(&observation)

			miss, ok := errors.AsType[*MissError](Check("begin", []int64{7}, []Observation{observation}))
			if !ok {
				t.Fatal("Check() did not return *MissError")
			}

			want := []RowMiss{{ID: 7, Reason: tc.want, Status: "SENDING"}}
			if !slices.Equal(miss.Rows, want) {
				t.Fatalf("Rows = %+v, want %+v", miss.Rows, want)
			}
		})
	}
}

func TestCheckReportsPartialApplicationInRequestOrder(t *testing.T) {
	applied := matched(2)

	applied.Applied = true

	stale := matched(3)

	stale.Status, stale.OwnerMatches = "QUARANTINED", false

	err := Check("route_failures", []int64{3, 2, 1}, []Observation{applied, stale})

	miss, ok := errors.AsType[*MissError](err)
	if !ok {
		t.Fatalf("Check() = %v, want *MissError", err)
	}

	if miss.Action != "route_failures" || miss.Expected != 3 || miss.Applied != 1 {
		t.Fatalf("miss = %+v, want action route_failures expected 3 applied 1", miss)
	}

	if got := miss.UnappliedDeliveryIDs(); !slices.Equal(got, []int64{3, 1}) {
		t.Fatalf("UnappliedDeliveryIDs() = %v, want [3 1]", got)
	}

	if !miss.Has(ReasonOwnerChanged) || !miss.Has(ReasonMissing) || miss.Has(ReasonStatusChanged) {
		t.Fatalf("Rows = %+v, want owner_changed for 3 and missing for 1 only", miss.Rows)
	}
}

func TestCheckIgnoresObservationsOutsideRequestedTargets(t *testing.T) {
	requested := matched(1)

	requested.Applied = true

	if err := Check("mark_sent", []int64{1}, []Observation{requested, matched(9)}); err != nil {
		t.Fatalf("Check() = %v, want nil for unrequested observation", err)
	}
}

func TestReasonOwnershipHeldOnlyWhenRowFenceStillMatches(t *testing.T) {
	held := []Reason{ReasonMemberTerminal, ReasonMembershipChanged, ReasonRequestChanged, ReasonPeerMissed}
	lost := []Reason{ReasonMissing, ReasonStatusChanged, ReasonOwnerChanged, ReasonVersionChanged, ReasonLeaseExpired}

	for _, reason := range held {
		if !reason.OwnershipHeld() {
			t.Errorf("%s.OwnershipHeld() = false, want true", reason)
		}
	}

	for _, reason := range lost {
		if reason.OwnershipHeld() {
			t.Errorf("%s.OwnershipHeld() = true, want false", reason)
		}
	}
}
