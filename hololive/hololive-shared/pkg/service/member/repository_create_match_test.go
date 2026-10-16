package member

import (
	"errors"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCreateMemberPreservesDuplicateErrorForExistingCallers(t *testing.T) {
	repository, _ := newPGXRepository(t)
	first := &domain.Member{Name: "Ordinary Create"}

	if err := repository.CreateMember(t.Context(), first); err != nil {
		t.Fatal(err)
	}

	if first.ID <= 0 {
		t.Fatal("create did not return committed member ID")
	}

	if err := repository.CreateMember(t.Context(), &domain.Member{Name: first.Name}); err == nil {
		t.Fatal("ordinary create must not silently replay a duplicate")
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
			if err := repository.CreateMember(t.Context(), created); err != nil {
				t.Fatal(err)
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
