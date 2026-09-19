package dispatchops

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var revisionTime = time.Date(2026, 9, 18, 1, 2, 3, 456789000, time.UTC)

func validRequest() RequeueRequest {
	return RequeueRequest{OperatorID: "operator-1", Reason: "원인 수정 후 확인", DuplicateRiskAck: true,
		Targets: []Revision{{ID: "1", UpdatedAt: revisionTime}}}
}

func delivery(id, unit, status string) Delivery {
	return Delivery{ID: id, RoomID: "9007199254740993", SendUnitID: unit, Status: status, UpdatedAt: revisionTime}
}

func TestParseID(t *testing.T) {
	for _, id := range []string{"1", "9007199254740993", "9223372036854775807"} {
		t.Run("accept_"+id, func(t *testing.T) {
			if _, err := ParseID(id); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, id := range []string{"", "0", "-1", "+1", "01", " 1", "1 ", "1.0", "1e3", "１", "9223372036854775808", "18446744073709551615", "1\x00"} {
		t.Run("reject_"+id, func(t *testing.T) {
			if _, err := ParseID(id); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("id %q: %v", id, err)
			}
		})
	}
}

func TestFilterValidation(t *testing.T) {
	for _, status := range append(statuses[:], "") {
		if err := (Filter{Status: status, RoomID: "9007199254740993", ChannelID: "UC-example", BeforeID: "9223372036854775807"}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []Filter{{Status: "all"}, {Status: "DLQ"}, {Status: "dlq' OR TRUE--"}, {BeforeID: "01"},
		{RoomID: strings.Repeat("가", 101)}, {RoomID: "\troom"}, {ChannelID: strings.Repeat("a", 65)}, {ChannelID: "a\n"}} {
		if err := f.Validate(); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("filter %+v: %v", f, err)
		}
	}
}

func TestRequestValidation(t *testing.T) {
	tests := []struct {
		name   string
		change func(*RequeueRequest)
	}{
		{"missing_ack", func(r *RequeueRequest) { r.DuplicateRiskAck = false }},
		{"missing_operator", func(r *RequeueRequest) { r.OperatorID = "" }},
		{"blank_operator", func(r *RequeueRequest) { r.OperatorID = "  " }},
		{"oversized_operator", func(r *RequeueRequest) { r.OperatorID = strings.Repeat("가", 129) }},
		{"missing_reason", func(r *RequeueRequest) { r.Reason = "" }},
		{"blank_reason", func(r *RequeueRequest) { r.Reason = "\u2003" }},
		{"oversized_reason", func(r *RequeueRequest) { r.Reason = strings.Repeat("가", 1025) }},
		{"control_reason", func(r *RequeueRequest) { r.Reason = "one\ntwo" }},
		{"invalid_utf8", func(r *RequeueRequest) { r.Reason = string([]byte{0xff}) }},
		{"no_targets", func(r *RequeueRequest) { r.Targets = nil }},
		{"too_many_targets", func(r *RequeueRequest) { r.Targets = make([]Revision, MaxReplaySize+1) }},
		{"duplicate_target", func(r *RequeueRequest) { r.Targets = append(r.Targets, r.Targets[0]) }},
		{"wrong_target", func(r *RequeueRequest) { r.Targets[0].ID = "2" }},
		{"noncanonical_target", func(r *RequeueRequest) { r.Targets[0].ID = "01" }},
		{"missing_revision", func(r *RequeueRequest) { r.Targets[0].UpdatedAt = time.Time{} }},
		{"invalid_year", func(r *RequeueRequest) { r.Targets[0].UpdatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validRequest()
			test.change(&request)
			if err := request.Validate("1"); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("got %v", err)
			}
		})
	}
	r := validRequest()
	r.OperatorID = strings.Repeat("가", 128)
	r.Reason = strings.Repeat("가", 1024)
	if err := r.Validate("1"); err != nil {
		t.Fatal(err)
	}
	if err := r.Validate("01"); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
}

func TestReplayRejectsEveryNonFailureState(t *testing.T) {
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			group := []Delivery{delivery("1", "", status)}
			err := validateReplay(group, validRequest())
			allowed := status == "dlq" || status == "quarantined"
			if allowed && err != nil {
				t.Fatal(err)
			}
			if !allowed && !errors.Is(err, ErrConflict) {
				t.Fatalf("state %s accepted: %v", status, err)
			}
		})
	}
}

func TestReplayRequiresExactGroupAndRevision(t *testing.T) {
	group := []Delivery{delivery("1", "10", "dlq"), delivery("2", "10", "quarantined")}
	r := validRequest()
	if !errors.Is(validateReplay(group, r), ErrConflict) {
		t.Fatal("partial group accepted")
	}
	r.Targets = append(r.Targets, Revision{ID: "2", UpdatedAt: revisionTime})
	if err := validateReplay(group, r); err != nil {
		t.Fatal(err)
	}
	r.Targets[0].UpdatedAt = revisionTime.In(time.FixedZone("KST", 9*60*60))
	if err := validateReplay(group, r); err != nil {
		t.Fatal("equivalent timestamp rejected", err)
	}
	r.Targets[0].UpdatedAt = revisionTime.Truncate(time.Millisecond)
	if !errors.Is(validateReplay(group, r), ErrConflict) {
		t.Fatal("lost microseconds accepted")
	}
	r.Targets[0].UpdatedAt = revisionTime.Add(time.Microsecond)
	if !errors.Is(validateReplay(group, r), ErrConflict) {
		t.Fatal("stale revision accepted")
	}
	r.Targets[0].UpdatedAt = revisionTime
	r.Targets[1].ID = "3"
	if !errors.Is(validateReplay(group, r), ErrConflict) {
		t.Fatal("different group member accepted")
	}
}

func TestReplayBlocksMixedAndOversizedGroups(t *testing.T) {
	tests := []struct {
		name  string
		group []Delivery
		block string
	}{
		{"empty", nil, "not_found"},
		{"active_member", []Delivery{delivery("1", "10", "dlq"), delivery("2", "10", "sending")}, "group_not_terminal_failure"},
		{"sent_member", []Delivery{delivery("1", "10", "dlq"), delivery("2", "10", "sent")}, "group_not_terminal_failure"},
		{"different_room", []Delivery{delivery("1", "10", "dlq"), {ID: "2", RoomID: "other", SendUnitID: "10", Status: "dlq"}}, "group_identity_mismatch"},
		{"different_unit", []Delivery{delivery("1", "10", "dlq"), delivery("2", "11", "dlq")}, "group_identity_mismatch"},
		{"multiple_legacy", []Delivery{delivery("1", "", "dlq"), delivery("2", "", "dlq")}, "group_identity_mismatch"},
		{"oversized", make([]Delivery, MaxReplaySize+1), "group_too_large"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := replayBlock(test.group); got != test.block {
				t.Fatalf("got %q want %q", got, test.block)
			}
		})
	}
	group := make([]Delivery, 0, MaxReplaySize)
	r := validRequest()
	r.Targets = make([]Revision, 0, MaxReplaySize)
	for i := 1; i <= MaxReplaySize; i++ {
		id := fmt.Sprint(i)
		group = append(group, delivery(id, "10", "dlq"))
		r.Targets = append(r.Targets, Revision{ID: id, UpdatedAt: revisionTime})
	}
	if err := r.Validate("1"); err != nil {
		t.Fatal(err)
	}
	if err := validateReplay(group, r); err != nil {
		t.Fatal(err)
	}
}

func TestReplayDoesNotMutateInputs(t *testing.T) {
	group := []Delivery{delivery("1", "", "dlq")}
	r := validRequest()
	if err := validateReplay(group, r); err != nil {
		t.Fatal(err)
	}
	if group[0].Status != "dlq" || group[0].UpdatedAt != revisionTime || r.Targets[0].UpdatedAt != revisionTime {
		t.Fatal("validation mutated caller state")
	}
}

func TestReplayRejectsPriorSentOrCancelledMarker(t *testing.T) {
	for _, marker := range []string{"sent", "cancelled"} {
		item := delivery("1", "", "dlq")
		if marker == "sent" {
			item.SentAt = &revisionTime
		} else {
			item.CancelledAt = &revisionTime
		}
		if !errors.Is(validateReplay([]Delivery{item}, validRequest()), ErrConflict) {
			t.Fatalf("prior %s marker accepted", marker)
		}
	}
}

func FuzzParseID(f *testing.F) {
	for _, seed := range []string{"1", "0", "01", "9007199254740993", "9223372036854775807", "9223372036854775808", "+1", "한글"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		id, err := ParseID(value)
		if err == nil && (id <= 0 || fmt.Sprint(id) != value) {
			t.Fatalf("accepted noncanonical ID %q as %d", value, id)
		}
		if err != nil && !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
