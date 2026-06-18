package member

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeMemberRows struct {
	index   int
	rows    []fakeMemberRow
	err     error
	closed  bool
	scanned bool
}

type fakeMemberRow struct {
	scan func(dest ...any) error
}

func assignScanDest[T any](dest any, value T) {
	ptr, ok := dest.(*T)
	if !ok {
		panic(fmt.Sprintf("scan destination type %T does not match value type %T", dest, value))
	}
	*ptr = value
}

func (r *fakeMemberRows) Close() { r.closed = true }

func (r *fakeMemberRows) Err() error { return r.err }

func (r *fakeMemberRows) CommandTag() pgconn.CommandTag { return pgconn.NewCommandTag("") }

func (r *fakeMemberRows) FieldDescriptions() []pgconn.FieldDescription { return nil }

func (r *fakeMemberRows) Next() bool {
	if r.index >= len(r.rows) {
		r.closed = true
		return false
	}
	r.scanned = false
	r.index++
	return true
}

func (r *fakeMemberRows) Scan(dest ...any) error {
	if r.index == 0 || r.index > len(r.rows) {
		return errors.New("scan called without row")
	}
	r.scanned = true
	return r.rows[r.index-1].scan(dest...)
}

func (r *fakeMemberRows) Values() ([]any, error) { return nil, errors.New("not implemented") }

func (r *fakeMemberRows) RawValues() [][]byte { return nil }

func (r *fakeMemberRows) Conn() *pgx.Conn { return nil }

func newTestMemberRepository() *Repository {
	return &Repository{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestCollectAllMembersFromRows_PreservesShortKoreanName(t *testing.T) {
	repository := newTestMemberRepository()
	rows := &fakeMemberRows{rows: []fakeMemberRow{
		{scan: func(dest ...any) error {
			if len(dest) != 15 {
				return errors.New("scan destination count mismatch")
			}
			assignScanDest[int](dest[0], 1)
			assignScanDest[string](dest[1], "ookami-mio")
			channelID := "UC_MIO"
			assignScanDest[*string](dest[2], &channelID)
			assignScanDest[string](dest[3], "Ookami Mio")
			assignScanDest[*string](dest[4], nil)
			koreanName := "오오카미 미오"
			assignScanDest[*string](dest[5], &koreanName)
			shortKoreanName := "미오"
			assignScanDest[*string](dest[6], &shortKoreanName)
			assignScanDest[string](dest[7], "active")
			assignScanDest[bool](dest[8], false)
			assignScanDest[[]byte](dest[9], []byte(`{"ko":["미오"]}`))
			assignScanDest[*string](dest[10], nil)
			assignScanDest[string](dest[11], "hololive")
			assignScanDest[*string](dest[12], nil)
			assignScanDest[string](dest[13], "holodex")
			assignScanDest[*string](dest[14], nil)
			return nil
		}},
	}}

	members, err := repository.collectAllMembersFromRows(rows)
	if err != nil {
		t.Fatalf("collectAllMembersFromRows error = %v, want nil", err)
	}
	if len(members) != 1 || members[0] == nil {
		t.Fatalf("members = %#v, want one member", members)
	}
	if members[0].ShortKoreanName != "미오" {
		t.Fatalf("ShortKoreanName = %q, want 미오", members[0].ShortKoreanName)
	}
}

func TestCollectAllMembersFromRows_ReturnsJoinedRowErrors(t *testing.T) {
	repository := newTestMemberRepository()
	rows := &fakeMemberRows{rows: []fakeMemberRow{
		{scan: func(dest ...any) error {
			assignScanDest[int](dest[0], 1)
			assignScanDest[string](dest[1], "suisei")
			channelID := "UC1"
			assignScanDest[*string](dest[2], &channelID)
			assignScanDest[string](dest[3], "Suisei")
			assignScanDest[*string](dest[4], nil)
			assignScanDest[*string](dest[5], nil)
			assignScanDest[*string](dest[6], nil)
			assignScanDest[string](dest[7], "active")
			assignScanDest[bool](dest[8], false)
			assignScanDest[[]byte](dest[9], []byte("not-json"))
			assignScanDest[*string](dest[10], nil)
			assignScanDest[string](dest[11], "hololive")
			assignScanDest[*string](dest[12], nil)
			assignScanDest[string](dest[13], "holodex")
			assignScanDest[*string](dest[14], nil)
			return nil
		}},
		{scan: func(dest ...any) error {
			assignScanDest[int](dest[0], 2)
			assignScanDest[string](dest[1], "miko")
			channelID := "UC2"
			assignScanDest[*string](dest[2], &channelID)
			assignScanDest[string](dest[3], "Miko")
			assignScanDest[*string](dest[4], nil)
			assignScanDest[*string](dest[5], nil)
			assignScanDest[*string](dest[6], nil)
			assignScanDest[string](dest[7], "active")
			assignScanDest[bool](dest[8], false)
			assignScanDest[[]byte](dest[9], []byte(`{"ko":["미코"]}`))
			assignScanDest[*string](dest[10], nil)
			assignScanDest[string](dest[11], "hololive")
			assignScanDest[*string](dest[12], nil)
			assignScanDest[string](dest[13], "holodex")
			assignScanDest[*string](dest[14], nil)
			return nil
		}},
	}}

	members, err := repository.collectAllMembersFromRows(rows)
	if err == nil {
		t.Fatal("collectAllMembersFromRows error = nil, want non-nil")
	}
	if len(members) != 1 || members[0] == nil || members[0].ChannelID != "UC2" {
		t.Fatalf("members = %#v, want one valid member for UC2", members)
	}
	if got := err.Error(); got == "" || !containsAll(got, []string{"failed to parse member row", "Suisei", "failed to unmarshal aliases"}) {
		t.Fatalf("error = %q, want joined parse error context", got)
	}
}

func TestCollectMembersWithPhotoFromRows_ReturnsJoinedRowErrors(t *testing.T) {
	repository := newTestMemberRepository()
	rows := &fakeMemberRows{rows: []fakeMemberRow{
		{scan: func(dest ...any) error {
			return errors.New("scan mismatch")
		}},
		{scan: func(dest ...any) error {
			assignScanDest[int](dest[0], 2)
			channelID := "UC2"
			assignScanDest[*string](dest[1], &channelID)
			assignScanDest[string](dest[2], "Miko")
			assignScanDest[*string](dest[3], nil)
			assignScanDest[*string](dest[4], nil)
			assignScanDest[*string](dest[5], nil)
			assignScanDest[bool](dest[6], false)
			assignScanDest[[]byte](dest[7], []byte(`{"ko":["미코"]}`))
			photo := "https://example.com/miko.jpg"
			assignScanDest[*string](dest[8], &photo)
			assignScanDest[string](dest[9], "hololive")
			assignScanDest[*string](dest[10], nil)
			assignScanDest[string](dest[11], "holodex")
			assignScanDest[*string](dest[12], nil)
			return nil
		}},
	}}

	members, err := repository.collectMembersWithPhotoFromRows(rows)
	if err == nil {
		t.Fatal("collectMembersWithPhotoFromRows error = nil, want non-nil")
	}
	member := members["UC2"]
	if member == nil || member.Photo != "https://example.com/miko.jpg" {
		t.Fatalf("members = %#v, want UC2 photo member", members)
	}
	if got := err.Error(); got == "" || !containsAll(got, []string{"failed to scan member row", "scan mismatch"}) {
		t.Fatalf("error = %q, want joined scan error context", got)
	}
}

func TestCollectMembersByNameFromRows_ReturnsJoinedRowErrors(t *testing.T) {
	repository := newTestMemberRepository()
	rows := &fakeMemberRows{rows: []fakeMemberRow{
		{scan: func(dest ...any) error {
			assignScanDest[int](dest[0], 1)
			assignScanDest[string](dest[1], "suisei")
			channelID := "UC1"
			assignScanDest[*string](dest[2], &channelID)
			assignScanDest[string](dest[3], "Suisei")
			assignScanDest[*string](dest[4], nil)
			assignScanDest[*string](dest[5], nil)
			assignScanDest[*string](dest[6], nil)
			assignScanDest[string](dest[7], "active")
			assignScanDest[bool](dest[8], false)
			assignScanDest[[]byte](dest[9], []byte(`{"ko":["스이세이"]}`))
			assignScanDest[string](dest[10], "hololive")
			assignScanDest[*string](dest[11], nil)
			assignScanDest[string](dest[12], "holodex")
			assignScanDest[*string](dest[13], nil)
			return nil
		}},
		{scan: func(dest ...any) error {
			assignScanDest[int](dest[0], 2)
			assignScanDest[string](dest[1], "miko")
			channelID := "UC2"
			assignScanDest[*string](dest[2], &channelID)
			assignScanDest[string](dest[3], "Miko")
			assignScanDest[*string](dest[4], nil)
			assignScanDest[*string](dest[5], nil)
			assignScanDest[*string](dest[6], nil)
			assignScanDest[string](dest[7], "active")
			assignScanDest[bool](dest[8], false)
			assignScanDest[[]byte](dest[9], []byte("not-json"))
			assignScanDest[string](dest[10], "hololive")
			assignScanDest[*string](dest[11], nil)
			assignScanDest[string](dest[12], "holodex")
			assignScanDest[*string](dest[13], nil)
			return nil
		}},
	}}

	members, err := repository.collectMembersByNameFromRows(rows)
	if err == nil {
		t.Fatal("collectMembersByNameFromRows error = nil, want non-nil")
	}
	if len(members) != 1 || members[0] == nil || members[0].ChannelID != "UC1" {
		t.Fatalf("members = %#v, want one valid member for UC1", members)
	}
	if got := err.Error(); got == "" || !containsAll(got, []string{"failed to parse member row", "Miko", "failed to unmarshal aliases"}) {
		t.Fatalf("error = %q, want joined parse error context", got)
	}
}

func TestUpgradePhotoResolution_ReplacesKnownSizeWithSingleStandardSuffix(t *testing.T) {
	if got := UpgradePhotoResolution("https://example.com/avatar=s88=s240"); got != "https://example.com/avatar=s1024=s240" {
		t.Fatalf("UpgradePhotoResolution() = %q", got)
	}
}

func containsAll(got string, wants []string) bool {
	for _, want := range wants {
		if !strings.Contains(got, want) {
			return false
		}
	}
	return true
}
