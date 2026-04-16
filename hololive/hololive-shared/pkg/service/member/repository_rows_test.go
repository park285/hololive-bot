package member

import (
	"errors"
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

func TestCollectAllMembersFromRows_ReturnsJoinedRowErrors(t *testing.T) {
	repo := newTestMemberRepository()
	rows := &fakeMemberRows{rows: []fakeMemberRow{
		{scan: func(dest ...any) error {
			*dest[0].(*int) = 1
			*dest[1].(*string) = "suisei"
			channelID := "UC1"
			*dest[2].(**string) = &channelID
			*dest[3].(*string) = "Suisei"
			*dest[4].(**string) = nil
			*dest[5].(**string) = nil
			*dest[6].(*string) = "active"
			*dest[7].(*bool) = false
			*dest[8].(*[]byte) = []byte("not-json")
			*dest[9].(**string) = nil
			*dest[10].(*string) = "hololive"
			*dest[11].(**string) = nil
			*dest[12].(*string) = "holodex"
			*dest[13].(**string) = nil
			return nil
		}},
		{scan: func(dest ...any) error {
			*dest[0].(*int) = 2
			*dest[1].(*string) = "miko"
			channelID := "UC2"
			*dest[2].(**string) = &channelID
			*dest[3].(*string) = "Miko"
			*dest[4].(**string) = nil
			*dest[5].(**string) = nil
			*dest[6].(*string) = "active"
			*dest[7].(*bool) = false
			*dest[8].(*[]byte) = []byte(`{"ko":["미코"]}`)
			*dest[9].(**string) = nil
			*dest[10].(*string) = "hololive"
			*dest[11].(**string) = nil
			*dest[12].(*string) = "holodex"
			*dest[13].(**string) = nil
			return nil
		}},
	}}

	members, err := repo.collectAllMembersFromRows(rows)
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
	repo := newTestMemberRepository()
	rows := &fakeMemberRows{rows: []fakeMemberRow{
		{scan: func(dest ...any) error {
			return errors.New("scan mismatch")
		}},
		{scan: func(dest ...any) error {
			*dest[0].(*int) = 2
			channelID := "UC2"
			*dest[1].(**string) = &channelID
			*dest[2].(*string) = "Miko"
			*dest[3].(**string) = nil
			*dest[4].(**string) = nil
			*dest[5].(*bool) = false
			*dest[6].(*[]byte) = []byte(`{"ko":["미코"]}`)
			photo := "https://example.com/miko.jpg"
			*dest[7].(**string) = &photo
			*dest[8].(*string) = "hololive"
			*dest[9].(**string) = nil
			*dest[10].(*string) = "holodex"
			*dest[11].(**string) = nil
			return nil
		}},
	}}

	members, err := repo.collectMembersWithPhotoFromRows(rows)
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
	repo := newTestMemberRepository()
	rows := &fakeMemberRows{rows: []fakeMemberRow{
		{scan: func(dest ...any) error {
			*dest[0].(*int) = 1
			*dest[1].(*string) = "suisei"
			channelID := "UC1"
			*dest[2].(**string) = &channelID
			*dest[3].(*string) = "Suisei"
			*dest[4].(**string) = nil
			*dest[5].(**string) = nil
			*dest[6].(*string) = "active"
			*dest[7].(*bool) = false
			*dest[8].(*[]byte) = []byte(`{"ko":["스이세이"]}`)
			*dest[9].(*string) = "hololive"
			*dest[10].(**string) = nil
			*dest[11].(*string) = "holodex"
			*dest[12].(**string) = nil
			return nil
		}},
		{scan: func(dest ...any) error {
			*dest[0].(*int) = 2
			*dest[1].(*string) = "miko"
			channelID := "UC2"
			*dest[2].(**string) = &channelID
			*dest[3].(*string) = "Miko"
			*dest[4].(**string) = nil
			*dest[5].(**string) = nil
			*dest[6].(*string) = "active"
			*dest[7].(*bool) = false
			*dest[8].(*[]byte) = []byte("not-json")
			*dest[9].(*string) = "hololive"
			*dest[10].(**string) = nil
			*dest[11].(*string) = "holodex"
			*dest[12].(**string) = nil
			return nil
		}},
	}}

	members, err := repo.collectMembersByNameFromRows(rows)
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
