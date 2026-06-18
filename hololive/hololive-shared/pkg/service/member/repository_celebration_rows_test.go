package member

import (
	"errors"
	"testing"
	"time"
)

func scanFullCelebrationRow(dest []any, id int, name, channelID string, aliases []byte) {
	assignScanDest[int](dest[0], id)
	assignScanDest[string](dest[1], "slug")
	cid := channelID
	assignScanDest[*string](dest[2], &cid)
	assignScanDest[string](dest[3], name)
	assignScanDest[*string](dest[4], nil)
	assignScanDest[*string](dest[5], nil)
	assignScanDest[*string](dest[6], nil)
	assignScanDest[string](dest[7], "active")
	assignScanDest[bool](dest[8], false)
	assignScanDest[[]byte](dest[9], aliases)
	assignScanDest[*string](dest[10], nil)
	assignScanDest[string](dest[11], "hololive")
	assignScanDest[*string](dest[12], nil)
	assignScanDest[string](dest[13], "holodex")
	assignScanDest[*string](dest[14], nil)
	assignScanDest[*time.Time](dest[15], nil)
	assignScanDest[*time.Time](dest[16], nil)
}

func TestCollectCelebrationMembersFromRows_ReturnsJoinedRowErrors(t *testing.T) {
	repository := newTestMemberRepository()
	rows := &fakeMemberRows{rows: []fakeMemberRow{
		{scan: func(dest ...any) error {
			scanFullCelebrationRow(dest, 1, "Suisei", "UC1", []byte("not-json"))
			return nil
		}},
		{scan: func(dest ...any) error {
			scanFullCelebrationRow(dest, 2, "Miko", "UC2", []byte(`{"ko":["미코"]}`))
			return nil
		}},
	}}

	members, err := repository.collectCelebrationMembersFromRows(rows)
	if err == nil {
		t.Fatal("collectCelebrationMembersFromRows error = nil, want non-nil")
	}
	if len(members) != 1 || members[0] == nil || members[0].ChannelID != "UC2" {
		t.Fatalf("members = %#v, want one valid member for UC2", members)
	}
	if got := err.Error(); !containsAll(got, []string{"parse celebration member row", "Suisei", "failed to unmarshal aliases"}) {
		t.Fatalf("error = %q, want joined parse error context", got)
	}
}

func TestCollectCelebrationMembersFromRows_JoinsRowsErr(t *testing.T) {
	repository := newTestMemberRepository()
	rows := &fakeMemberRows{
		err: errors.New("connection reset"),
		rows: []fakeMemberRow{
			{scan: func(dest ...any) error {
				scanFullCelebrationRow(dest, 1, "Suisei", "UC1", []byte(`{"ko":["스이세이"]}`))
				return nil
			}},
		},
	}

	members, err := repository.collectCelebrationMembersFromRows(rows)
	if err == nil {
		t.Fatal("collectCelebrationMembersFromRows error = nil, want non-nil")
	}
	if len(members) != 1 || members[0].ChannelID != "UC1" {
		t.Fatalf("members = %#v, want the one successfully scanned member", members)
	}
	if got := err.Error(); !containsAll(got, []string{"celebration member rows iteration", "connection reset"}) {
		t.Fatalf("error = %q, want rows.Err context joined", got)
	}
}

func TestCollectCalendarEntriesFromRows_ReturnsJoinedRowErrors(t *testing.T) {
	repository := newTestMemberRepository()
	rows := &fakeMemberRows{rows: []fakeMemberRow{
		{scan: func(dest ...any) error {
			scanFullCelebrationRow(dest, 1, "Suisei", "UC1", []byte("not-json"))
			assignScanDest[string](dest[17], "birthday")
			assignScanDest[int](dest[18], 3)
			return nil
		}},
		{scan: func(dest ...any) error {
			scanFullCelebrationRow(dest, 2, "Miko", "UC2", []byte(`{"ko":["미코"]}`))
			assignScanDest[string](dest[17], "birthday")
			assignScanDest[int](dest[18], 4)
			return nil
		}},
	}}

	entries, err := repository.collectCalendarEntriesFromRows(rows, 2026)
	if err == nil {
		t.Fatal("collectCalendarEntriesFromRows error = nil, want non-nil")
	}
	if len(entries) != 1 || entries[0].Member == nil || entries[0].Member.ChannelID != "UC2" {
		t.Fatalf("entries = %#v, want one valid entry for UC2", entries)
	}
	if entries[0].Day != 4 {
		t.Fatalf("entries[0].Day = %d, want 4", entries[0].Day)
	}
	if got := err.Error(); !containsAll(got, []string{"parse calendar member row", "Suisei", "failed to unmarshal aliases"}) {
		t.Fatalf("error = %q, want joined parse error context", got)
	}
}

func TestCollectCalendarEntriesFromRows_JoinsRowsErr(t *testing.T) {
	repository := newTestMemberRepository()
	rows := &fakeMemberRows{
		err: errors.New("connection reset"),
		rows: []fakeMemberRow{
			{scan: func(dest ...any) error {
				scanFullCelebrationRow(dest, 1, "Suisei", "UC1", []byte(`{"ko":["스이세이"]}`))
				assignScanDest[string](dest[17], "birthday")
				assignScanDest[int](dest[18], 3)
				return nil
			}},
		},
	}

	entries, err := repository.collectCalendarEntriesFromRows(rows, 2026)
	if err == nil {
		t.Fatal("collectCalendarEntriesFromRows error = nil, want non-nil")
	}
	if len(entries) != 1 || entries[0].Member.ChannelID != "UC1" {
		t.Fatalf("entries = %#v, want the one successfully scanned entry", entries)
	}
	if got := err.Error(); !containsAll(got, []string{"calendar rows iteration", "connection reset"}) {
		t.Fatalf("error = %q, want rows.Err context joined", got)
	}
}
