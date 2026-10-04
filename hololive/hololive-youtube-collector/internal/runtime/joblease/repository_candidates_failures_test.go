package joblease

import (
	"errors"
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

type failedProjectionRows struct {
	err error
}

func (*failedProjectionRows) Next() bool        { return false }
func (*failedProjectionRows) Scan(...any) error { return nil }
func (r *failedProjectionRows) Err() error      { return r.err }

func TestCandidateReadFailureDoesNotReturnPartialPage(t *testing.T) {
	t.Parallel()

	cause := errors.New("후보 조회 스트림 중단")
	job := mustTestJob(t, contract.ProviderYouTubeJS, "community_collect")
	page, err := collectCandidatePage(&failedProjectionRows{err: cause}, job, 1)

	if !errors.Is(err, cause) || errors.Is(err, ErrCandidateContract) || len(page.Jobs) != 0 {
		t.Fatalf("후보 조회 오류를 숨겼습니다: page=%v err=%v", page, err)
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
