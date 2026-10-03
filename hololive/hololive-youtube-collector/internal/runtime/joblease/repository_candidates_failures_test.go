package joblease

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

func TestMixedBundleFailureIsLocalAndPreservesHealthyRunner(t *testing.T) {
	pool := dbtest.NewPool(t)
	generation := seedProjection(t, pool, []leaseTarget{
		{subjectUCA, contract.KindChannelProfile, 2 * time.Minute, true},
		{subjectUCA, contract.KindChannelPhoto, time.Minute, true},
		{subjectChannelA, contract.KindCommunityPage, time.Minute, true},
	})
	repository := newTestRepository(t, pool)
	broken := mustTestJob(t, contract.ProviderYouTubeJS, "youtubejs_channel_metadata")
	healthy := mustTestJob(t, contract.ProviderYouTubeJS, "community_collect")

	for range 4 {
		page, err := repository.CandidatesForProjection(t.Context(), generation, broken, nil, 4)
		if !errors.Is(err, ErrCandidateContract) || collecterr.ClassOf(err) != collecterr.ClassInternal || len(page.Jobs) != 0 {
			t.Fatalf("invalid bundle: page=%+v err=%v", page, err)
		}

		page, err = repository.CandidatesForProjection(t.Context(), generation, healthy, nil, 4)
		if err != nil || len(page.Jobs) != 1 || page.Jobs[0].SubjectKey != subjectChannelA {
			t.Fatalf("healthy runner: page=%+v err=%v", page, err)
		}
	}

	_, err := repository.CandidatesForProjection(t.Context(), generation+1, broken, nil, 4)
	if !errors.Is(err, collection.ErrProjectionStale) || errors.Is(err, ErrCandidateContract) {
		t.Fatalf("stale projection must remain global: %v", err)
	}
}

func TestCandidateRequestBudgetErrorsRemainGlobal(t *testing.T) {
	pool := dbtest.NewPool(t)
	generation := seedProjection(t, pool, []leaseTarget{{subjectChannelA, contract.KindCommunityPage, time.Minute, true}})
	repository := newTestRepository(t, pool)
	job := mustTestJob(t, contract.ProviderYouTubeJS, "community_collect")
	_, err := repository.CandidatesForProjection(t.Context(), generation, job, nil, 0)

	if err == nil || errors.Is(err, ErrCandidateContract) {
		t.Fatalf("invalid query budget must remain global: %v", err)
	}

	_, err = repository.CandidatesForProjection(t.Context(), generation, job, []string{""}, 1)
	if err == nil || errors.Is(err, ErrCandidateContract) {
		t.Fatalf("invalid queue exclusion must remain global: %v", err)
	}
}

type failedCandidateRows struct {
	read bool
	err  error
}

func (r *failedCandidateRows) Next() bool {
	if r.read {
		return false
	}

	r.read = true

	return true
}

func (*failedCandidateRows) Scan(dest ...any) error {
	if len(dest) != 4 {
		return fmt.Errorf("scan candidate fixture: unexpected column count %d", len(dest))
	}

	current, ok := dest[0].(*bool)
	if !ok || current == nil {
		return errors.New("scan candidate fixture: invalid current destination")
	}

	subject, ok := dest[1].(*sql.NullString)
	if !ok || subject == nil {
		return errors.New("scan candidate fixture: invalid subject destination")
	}

	minMS, ok := dest[2].(*sql.NullInt64)
	if !ok || minMS == nil {
		return errors.New("scan candidate fixture: invalid minimum destination")
	}

	maxMS, ok := dest[3].(*sql.NullInt64)
	if !ok || maxMS == nil {
		return errors.New("scan candidate fixture: invalid maximum destination")
	}

	*current = true
	*subject = sql.NullString{String: subjectUCA, Valid: true}
	*minMS = sql.NullInt64{Int64: 60000, Valid: true}
	*maxMS = sql.NullInt64{Int64: 120000, Valid: true}

	return nil
}

func (r *failedCandidateRows) Err() error { return r.err }

func TestCandidateReadErrorWinsOverMixedBundle(t *testing.T) {
	t.Parallel()

	cause := errors.New("database connection lost while reading page")
	rows := &failedCandidateRows{err: cause}
	job := mustTestJob(t, contract.ProviderYouTubeJS, "youtubejs_channel_metadata")
	page, err := collectCandidatePage(rows, job, 4)

	if !errors.Is(err, cause) || errors.Is(err, ErrCandidateContract) || len(page.Jobs) != 0 {
		t.Fatalf("global read failure hidden: page=%+v err=%v", page, err)
	}
}

func TestGlobalRunnerMixedBundleFailureIsLocal(t *testing.T) {
	pool := dbtest.NewPool(t)
	generation := seedProjection(t, pool, []leaseTarget{
		{subjectChannelA, contract.KindLiveSnapshot, time.Minute, true},
		{subjectChannelB, contract.KindLiveSnapshot, 2 * time.Minute, true},
	})
	repository := newTestRepository(t, pool)
	job := mustTestJob(t, contract.ProviderHolodex, "holodex_live")
	page, err := repository.CandidatesForProjection(t.Context(), generation, job, nil, 4)

	if !errors.Is(err, ErrCandidateContract) || collecterr.ClassOf(err) != collecterr.ClassInternal || len(page.Jobs) != 0 {
		t.Fatalf("invalid global bundle: page=%+v err=%v", page, err)
	}
}
