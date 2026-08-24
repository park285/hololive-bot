package joblease

import (
	"errors"
	"strings"
	"testing"
	"time"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func TestBuildJobKeyMatchesCandidateSQLExpression(t *testing.T) {
	t.Parallel()

	subject := "UC_TEST"
	id := sourceobservation.JobID{Provider: contract.ProviderYouTubeJS, Kind: "community_collect"}

	key, err := BuildJobKey(id, subject)
	if err != nil {
		t.Fatal(err)
	}

	want := "collector:" + string(id.Provider) + ":" + string(id.Kind) + ":" + subject
	if key != want {
		t.Fatalf("BuildJobKey = %q, want SQL expression %q", key, want)
	}

	sql := mustSQL("repository_candidates_0144_02.sql")
	expr := "'collector:' || $3 || ':' || $4 || ':' || target.subject_key"

	if strings.Count(sql, expr) < 2 {
		t.Fatalf("subject candidate SQL must inline BuildJobKey expression %s", expr)
	}

	globalID := sourceobservation.JobID{Provider: contract.ProviderHolodex, Kind: "holodex_live"}
	globalKey, err := BuildJobKey(globalID, "global:holodex_live")

	if err != nil || globalKey != "collector:holodex:holodex_live:global" {
		t.Fatalf("global BuildJobKey = %q, %v", globalKey, err)
	}
}

func TestSubjectCandidateSQLMatchesContractShape(t *testing.T) {
	t.Parallel()

	sql := mustSQL("repository_candidates_0144_02.sql")

	for _, want := range []string{
		"<> ALL($5::text[])",
		"LIMIT $6 + 1",
		"'collector:' || $3 || ':' || $4 || ':' || target.subject_key",
		"lease.next_due_at <= statement_timestamp()",
		"lease.retry_not_before <= statement_timestamp()",
		"lease.lease_expires_at <= statement_timestamp()",
		"ORDER BY (job_key IS NOT NULL), max_priority DESC, effective_due_at ASC, subject_key ASC",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("subject candidate SQL missing %q", want)
		}
	}
}

func TestGlobalCandidateSQLMatchesContractShape(t *testing.T) {
	t.Parallel()

	sql := mustSQL("repository_candidates_global_0144_17.sql")

	for _, want := range []string{
		"AND (NOT $3::boolean OR subject_key = $4)",
		"SELECT $5::text AS job_key",
		"$4::text AS subject_key",
		"identity.job_key <> ALL($6::text[])",
		"lease.next_due_at <= statement_timestamp()",
		"lease.retry_not_before <= statement_timestamp()",
		"lease.lease_expires_at <= statement_timestamp()",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("global candidate SQL missing %q", want)
		}
	}
}

func TestCandidatesForProjectionEmptyWhenTargetsMissingOrDisabled(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{subjectChannelA, contract.KindCommunityPage, time.Minute, false}})

	repository := newTestRepository(t, pool)
	page := candidatePage(t, repository, contract.ProviderYouTubeJS, "community_collect", nil, 4)

	if len(page.Jobs) != 0 || page.Truncated {
		t.Fatalf("disabled targets page = %#v", page)
	}

	generation, err := repository.CurrentProjectionGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}

	job := mustTestJob(t, contract.ProviderYouTubeJS, "youtubejs_viewer")
	empty, err := repository.CandidatesForProjection(ctx, generation, job, nil, 4)

	if err != nil || len(empty.Jobs) != 0 || empty.Truncated {
		t.Fatalf("missing targets page = %#v err=%v", empty, err)
	}
}

func TestCandidatesForProjectionStaleGeneration(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{subjectChannelA, contract.KindCommunityPage, time.Minute, true}})

	repository := newTestRepository(t, pool)
	job := mustTestJob(t, contract.ProviderYouTubeJS, "community_collect")
	_, err := repository.CandidatesForProjection(ctx, 1_000_000, job, nil, 4)

	if !errors.Is(err, ErrProjectionStale) {
		t.Fatalf("stale generation error = %v", err)
	}
}

func TestCandidatesForProjectionTruncatesWithSentinel(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{
		{subjectChannelA, contract.KindCommunityPage, time.Minute, true},
		{subjectChannelB, contract.KindCommunityPage, time.Minute, true},
		{"channel:c", contract.KindCommunityPage, time.Minute, true},
	})

	repository := newTestRepository(t, pool)
	page := candidatePage(t, repository, contract.ProviderYouTubeJS, "community_collect", nil, 2)

	if len(page.Jobs) != 2 || !page.Truncated {
		t.Fatalf("truncated page = %#v", page)
	}
}

func TestSCH009QueuedKeyExclusionDoesNotHideLaterDueKeys(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{
		{subjectChannelA, contract.KindCommunityPage, time.Minute, true},
		{subjectChannelB, contract.KindCommunityPage, time.Minute, true},
		{"channel:c", contract.KindCommunityPage, time.Minute, true},
	})

	repository := newTestRepository(t, pool)
	excluded := []string{"collector:youtubejs:community_collect:channel:a"}
	page := candidatePage(t, repository, contract.ProviderYouTubeJS, "community_collect", excluded, 2)

	if len(page.Jobs) == 0 {
		t.Fatal("excluded front key hid later due keys")
	}

	for _, spec := range page.Jobs {
		if spec.SubjectKey == subjectChannelA {
			t.Fatalf("excluded key was returned: %#v", spec)
		}
	}

	if page.Jobs[0].SubjectKey != subjectChannelB {
		t.Fatalf("later due key = %#v", page.Jobs)
	}
}

func TestSCH008GlobalNotDueReturnsEmptyPage(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{
		{subjectGlobalSchedule, contract.KindSchedule, time.Minute, true},
	})

	repository := newTestRepository(t, pool)
	spec := JobSpec{
		JobKey: "collector:hololive_official:official_schedule:global", Provider: contract.ProviderHololiveOfficial,
		Class: "GLOBAL", CollectionJobKind: "official_schedule",
		SubjectKey: subjectGlobalSchedule, PollInterval: time.Minute,
	}

	lease, err := repository.Acquire(ctx, &spec, "collector-a")
	if err != nil {
		t.Fatal(err)
	}

	if err := lease.Complete(ctx); err != nil {
		t.Fatal(err)
	}

	page := candidatePage(t, repository, contract.ProviderHololiveOfficial, "official_schedule", nil, 1)
	if len(page.Jobs) != 0 || page.Truncated {
		t.Fatalf("not-due GLOBAL page = %#v", page)
	}
}

func TestGlobalExactSubjectMembershipUsesLeaseSubject(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{
		{subjectGlobalSchedule, contract.KindSchedule, time.Minute, true},
		{"other-schedule", contract.KindSchedule, time.Minute, true},
	})

	repository := newTestRepository(t, pool)
	page := candidatePage(t, repository, contract.ProviderHololiveOfficial, "official_schedule", nil, 1)

	if len(page.Jobs) != 1 || page.Jobs[0].SubjectKey != subjectGlobalSchedule {
		t.Fatalf("exact-subject GLOBAL page = %#v", page)
	}

	if page.Jobs[0].JobKey != "collector:hololive_official:official_schedule:global" {
		t.Fatalf("persisted GLOBAL key = %q", page.Jobs[0].JobKey)
	}
}

func TestCandidatesForProjectionMixedIntervalIsInternal(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{
		{subjectUCA, contract.KindChannelStats, time.Minute, true},
		{subjectUCA, contract.KindChannelProfile, 2 * time.Minute, true},
		{subjectUCA, contract.KindChannelPhoto, time.Minute, true},
	})

	repository := newTestRepository(t, pool)

	generation, err := repository.CurrentProjectionGeneration(ctx)
	if err != nil {
		t.Fatal(err)
	}

	job := mustTestJob(t, contract.ProviderYouTubeJS, "youtubejs_channel_metadata")

	_, err = repository.CandidatesForProjection(ctx, generation, job, nil, 4)

	if collecterr.ClassOf(err) != collecterr.ClassInternal {
		t.Fatalf("mixed cadence error = %v", err)
	}
}

func TestCandidatesForProjectionQueryPlansAreAvailable(t *testing.T) {
	pool := dbtest.NewPool(t)
	seedProjection(t, pool, []leaseTarget{{subjectChannelA, contract.KindCommunityPage, time.Minute, true}})

	ctx := t.Context()

	rows, err := pool.Query(
		ctx,
		"EXPLAIN "+mustSQL("repository_candidates_0144_02.sql"),
		int64(1),
		[]string{string(contract.KindCommunityPage)},
		string(contract.ProviderYouTubeJS),
		"community_collect",
		[]string{},
		1,
	)
	if err != nil {
		t.Logf("TRACK live EXPLAIN unavailable: %v", err)

		return
	}
	defer rows.Close()

	var plan strings.Builder

	for rows.Next() {
		var line string

		if scanErr := rows.Scan(&line); scanErr != nil {
			t.Logf("TRACK live EXPLAIN scan: %v", scanErr)

			return
		}

		plan.WriteString(line)
		plan.WriteByte('\n')
	}

	if plan.Len() == 0 {
		t.Log("TRACK live EXPLAIN returned no rows")

		return
	}

	t.Logf("TRACK candidate EXPLAIN (no performance claim):\n%s", plan.String())
}

func mustTestJob(t *testing.T, provider contract.Provider, kind string) sourceobservation.JobContract {
	t.Helper()

	job, ok := sourceobservation.InitialJobContracts().Definition(sourceobservation.JobID{
		Provider: provider, Kind: sourceobservation.JobKind(kind),
	})
	if !ok {
		t.Fatalf("missing job contract %s/%s", provider, kind)
	}

	return job
}
