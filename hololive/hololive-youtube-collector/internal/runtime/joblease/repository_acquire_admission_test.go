package joblease

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

// 선점 SQL이 입장을 합쳐도 오류 우선순위와 상세 사유를 보존하고 거절한 작업 행을 생성하지 않는다.
func TestAcquireAdmissionRejectsWithoutCreatingLease(t *testing.T) {
	for _, exact := range []bool{true, false} {
		t.Run(fmt.Sprintf("exact=%t", exact), func(t *testing.T) {
			checkAcquireAdmissionRejections(t, exact)
		})
	}
}

func checkAcquireAdmissionRejections(t *testing.T, exact bool) {
	t.Helper()

	pool := dbtest.NewPool(t)
	spec := communityJob()
	target := leaseTarget{subjectChannelA, contract.KindCommunityPage, time.Minute, true}

	if !exact {
		spec = holodexLiveJob()
		target = leaseTarget{subjectUCA, contract.KindLiveSnapshot, time.Minute, true}
	}

	generation := seedProjection(t, pool, []leaseTarget{target})
	measured, _ := measuredCandidatePool(t, pool)

	if _, err := measured.Exec(t.Context(), "SET plan_cache_mode = force_generic_plan"); err != nil {
		t.Fatal(err)
	}

	repository := newTestRepository(t, measured)

	for _, test := range []struct {
		name   string
		change string
		want   error
		detail string
	}{
		{"disabled", "enabled=FALSE", collection.ErrTargetDisabled, ""},
		{"cadence", "poll_interval_ms=120000", ErrInvalidJob, "target cadence does not match job"},
		{"null-member", "member_since_generation=NULL", ErrInvalidJob, "current target membership is unproven"},
		{"not-before", "not_before=clock_timestamp()+INTERVAL '1 hour'", ErrNotAcquired, ""},
		{"cadence-before-admission", "poll_interval_ms=120000, not_before=clock_timestamp()+INTERVAL '1 hour'", ErrInvalidJob, "target cadence does not match job"},
		{"admission-before-membership", "member_since_generation=NULL, not_before=clock_timestamp()+INTERVAL '1 hour'", ErrNotAcquired, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(t.Context(), `UPDATE youtube_collection_targets SET enabled=TRUE,
				poll_interval_ms=60000, member_since_generation=projection_generation, not_before=NULL
				WHERE projection_generation=$1`, generation); err != nil {
				t.Fatal(err)
			}

			if _, err := pool.Exec(t.Context(), "UPDATE youtube_collection_targets SET "+test.change+" WHERE projection_generation=$1", generation); err != nil {
				t.Fatal(err)
			}

			lease, err := repository.Acquire(t.Context(), spec, "collector-a")
			if lease != nil || !errors.Is(err, test.want) {
				t.Fatalf("lease=%v error=%v, want %v", lease, err, test.want)
			}

			if test.detail != "" && !strings.Contains(err.Error(), test.detail) {
				t.Fatalf("error=%v, want detail %q", err, test.detail)
			}

			var count int

			if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM youtube_collection_job_leases WHERE job_key=$1", spec.JobKey).Scan(&count); err != nil {
				t.Fatal(err)
			}

			if count != 0 {
				t.Fatalf("rejected acquisition created %d lease rows", count)
			}
		})
	}
}
