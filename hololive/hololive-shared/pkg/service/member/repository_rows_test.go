package member

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	testChannelUC1 = "UC1"
	testChannelUC2 = "UC2"
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

	if err := r.rows[r.index-1].scan(dest...); err != nil {
		return fmt.Errorf("scan: %w", err)
	}

	return nil
}

func (r *fakeMemberRows) Values() ([]any, error) { return nil, errors.New("not implemented") }

func (r *fakeMemberRows) RawValues() [][]byte { return nil }

func (r *fakeMemberRows) Conn() *pgx.Conn { return nil }

func (r *fakeMemberRows) TypeMap() *pgtype.Map { return pgtype.NewMap() }

func newTestMemberRepository() *Repository {
	return &Repository{logger: slog.New(slog.DiscardHandler)}
}

func TestCollectMembersFromRows_All_PreservesShortKoreanName(t *testing.T) {
	repository := newTestMemberRepository()
	rows := &fakeMemberRows{rows: []fakeMemberRow{
		{scan: func(dest ...any) error {
			if len(dest) != 20 {
				return errors.New("scan destination count mismatch")
			}

			assignScanDest(dest[0], 1)
			assignScanDest(dest[1], "ookami-mio")

			channelID := "UC_MIO"
			assignScanDest(dest[2], &channelID)
			assignScanDest(dest[3], "Ookami Mio")
			assignScanDest[*string](dest[4], nil)

			koreanName := "오오카미 미오"
			assignScanDest(dest[5], &koreanName)

			shortKoreanName := "미오"
			assignScanDest(dest[6], &shortKoreanName)
			assignScanDest(dest[7], "active")
			assignScanDest(dest[8], false)
			assignScanDest(dest[9], []byte(`{"ko":["미오"]}`))
			assignScanDest[*string](dest[10], nil)
			assignScanDest(dest[11], "hololive")
			assignScanDest[*string](dest[12], nil)
			assignScanDest(dest[13], "holodex")
			assignScanDest[*string](dest[14], nil)

			return nil
		}},
	}}

	members, err := repository.collectMembersFromRows(rows, memberIterLabel, "member", scanMemberFullRow)
	if err != nil {
		t.Fatalf("collectMembersFromRows error = %v, want nil", err)
	}

	if len(members) != 1 || members[0] == nil {
		t.Fatalf("members = %#v, want one member", members)
	}

	if members[0].ShortKoreanName != "미오" {
		t.Fatalf("ShortKoreanName = %q, want 미오", members[0].ShortKoreanName)
	}
}

func TestCollectMembersFromRows_All_ReturnsJoinedRowErrors(t *testing.T) {
	repository := newTestMemberRepository()
	rows := &fakeMemberRows{rows: []fakeMemberRow{
		{scan: func(dest ...any) error {
			assignScanDest(dest[0], 1)
			assignScanDest(dest[1], "suisei")

			channelID := testChannelUC1
			assignScanDest(dest[2], &channelID)
			assignScanDest(dest[3], "Suisei")
			assignScanDest[*string](dest[4], nil)
			assignScanDest[*string](dest[5], nil)
			assignScanDest[*string](dest[6], nil)
			assignScanDest(dest[7], "active")
			assignScanDest(dest[8], false)
			assignScanDest(dest[9], []byte("not-json"))
			assignScanDest[*string](dest[10], nil)
			assignScanDest(dest[11], "hololive")
			assignScanDest[*string](dest[12], nil)
			assignScanDest(dest[13], "holodex")
			assignScanDest[*string](dest[14], nil)

			return nil
		}},
		{scan: func(dest ...any) error {
			assignScanDest(dest[0], 2)
			assignScanDest(dest[1], testMemberMikoSlug)

			channelID := testChannelUC2
			assignScanDest(dest[2], &channelID)
			assignScanDest(dest[3], testMemberMiko)
			assignScanDest[*string](dest[4], nil)
			assignScanDest[*string](dest[5], nil)
			assignScanDest[*string](dest[6], nil)
			assignScanDest(dest[7], "active")
			assignScanDest(dest[8], false)
			assignScanDest(dest[9], []byte(`{"ko":["미코"]}`))
			assignScanDest[*string](dest[10], nil)
			assignScanDest(dest[11], "hololive")
			assignScanDest[*string](dest[12], nil)
			assignScanDest(dest[13], "holodex")
			assignScanDest[*string](dest[14], nil)

			return nil
		}},
	}}

	members, err := repository.collectMembersFromRows(rows, memberIterLabel, "member", scanMemberFullRow)
	if err == nil {
		t.Fatal("collectMembersFromRows error = nil, want non-nil")
	}

	if len(members) != 1 || members[0] == nil || members[0].ChannelID != testChannelUC2 {
		t.Fatalf("members = %#v, want one valid member for UC2", members)
	}

	if got := err.Error(); got == "" || !containsAll(got, []string{"parse member row", "Suisei", "unmarshal aliases"}) {
		t.Fatalf("error = %q, want joined parse error context", got)
	}
}

func TestCollectMembersFromRows_ByName_ReturnsJoinedRowErrors(t *testing.T) {
	repository := newTestMemberRepository()
	rows := &fakeMemberRows{rows: []fakeMemberRow{
		{scan: func(dest ...any) error {
			assignScanDest(dest[0], 1)
			assignScanDest(dest[1], "suisei")

			channelID := testChannelUC1
			assignScanDest(dest[2], &channelID)
			assignScanDest(dest[3], "Suisei")
			assignScanDest[*string](dest[4], nil)
			assignScanDest[*string](dest[5], nil)
			assignScanDest[*string](dest[6], nil)
			assignScanDest(dest[7], "active")
			assignScanDest(dest[8], false)
			assignScanDest(dest[9], []byte(`{"ko":["스이세이"]}`))
			assignScanDest(dest[10], "hololive")
			assignScanDest[*string](dest[11], nil)
			assignScanDest(dest[12], "holodex")
			assignScanDest[*string](dest[13], nil)

			return nil
		}},
		{scan: func(dest ...any) error {
			assignScanDest(dest[0], 2)
			assignScanDest(dest[1], testMemberMikoSlug)

			channelID := testChannelUC2
			assignScanDest(dest[2], &channelID)
			assignScanDest(dest[3], testMemberMiko)
			assignScanDest[*string](dest[4], nil)
			assignScanDest[*string](dest[5], nil)
			assignScanDest[*string](dest[6], nil)
			assignScanDest(dest[7], "active")
			assignScanDest(dest[8], false)
			assignScanDest(dest[9], []byte("not-json"))
			assignScanDest(dest[10], "hololive")
			assignScanDest[*string](dest[11], nil)
			assignScanDest(dest[12], "holodex")
			assignScanDest[*string](dest[13], nil)

			return nil
		}},
	}}

	members, err := repository.collectMembersFromRows(rows, memberIterLabel, "member", scanMemberQueryRow)
	if err == nil {
		t.Fatal("collectMembersFromRows error = nil, want non-nil")
	}

	if len(members) != 1 || members[0] == nil || members[0].ChannelID != testChannelUC1 {
		t.Fatalf("members = %#v, want one valid member for UC1", members)
	}

	if got := err.Error(); got == "" || !containsAll(got, []string{"parse member row", testMemberMiko, "unmarshal aliases"}) {
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
