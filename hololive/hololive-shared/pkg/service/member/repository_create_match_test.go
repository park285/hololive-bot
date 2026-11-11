package member

import (
	"errors"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCreateMemberOrMatchReplaysIdenticalMember(t *testing.T) {
	repository, _ := newPGXRepository(t)
	first := &domain.Member{Name: "Ordinary Create"}

	if replayed, err := repository.CreateMemberOrMatch(t.Context(), first); err != nil || replayed {
		t.Fatalf("first create replayed=%t err=%v; want new member", replayed, err)
	}

	if first.ID <= 0 {
		t.Fatal("create did not return committed member ID")
	}

	replayed := &domain.Member{Name: first.Name}
	matched, err := repository.CreateMemberOrMatch(t.Context(), replayed)

	if err != nil || !matched || replayed.ID != first.ID {
		t.Fatalf("explicit recovery matched=%t id=%d err=%v", matched, replayed.ID, err)
	}
}

func TestCreateMemberRecoveryComparesCanonicalDefaultsAndCurrentName(t *testing.T) {
	repository, pool := newPGXRepository(t)

	for _, tc := range []struct {
		name   string
		update string
	}{
		{"name changed", "UPDATE members SET english_name='changed' WHERE id=$1"},
		{"source changed", "UPDATE members SET sync_source='holodex' WHERE id=$1"},
		{"suborg changed", "UPDATE members SET suborg='JP' WHERE id=$1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			created := &domain.Member{Name: "Canonical " + tc.name}
			if replayed, err := repository.CreateMemberOrMatch(t.Context(), created); err != nil || replayed {
				t.Fatalf("create replayed=%t err=%v; want new member", replayed, err)
			}

			if _, err := pool.Exec(t.Context(), tc.update, created.ID); err != nil {
				t.Fatal(err)
			}

			matched, err := repository.CreateMemberOrMatch(t.Context(), &domain.Member{Name: created.Name})
			if matched || !errors.Is(err, ErrCreateConflict) {
				t.Fatalf("changed canonical row matched=%t err=%v", matched, err)
			}
		})
	}
}
