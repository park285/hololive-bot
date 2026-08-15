package targetprojection

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

var projectionNow = time.Date(2026, 8, 14, 2, 0, 0, 0, time.UTC)

type staticBuilder struct {
	targets []TargetSpec
	reasons []TargetReason
	err     error
}

func (b staticBuilder) Build(context.Context, dbx.Tx, time.Time) ([]TargetSpec, []TargetReason, error) {
	return b.targets, b.reasons, b.err
}

func TestRefreshProjectionPaths(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	refresher, err := NewRefresher(pool, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	first := TargetSpec{SubjectKey: "channel:a", ObservationKind: contract.KindCommunityPage, Priority: 50, PollInterval: time.Minute, Enabled: true}
	reason := TargetReason{SubjectKey: first.SubjectKey, ObservationKind: first.ObservationKind, ReasonKind: "notification_target", ReasonKey: "room:a"}

	created := mustRefresh(t, ctx, refresher, staticBuilder{targets: []TargetSpec{first}, reasons: []TargetReason{reason}}, projectionNow, "initial")
	if !created.Changed || created.Generation <= 0 || created.RowCount != 1 {
		t.Fatalf("initial result = %#v", created)
	}

	refreshed := mustRefresh(t, ctx, refresher, staticBuilder{targets: []TargetSpec{first, first}, reasons: []TargetReason{reason, reason}}, projectionNow.Add(10*time.Minute), "same projection")
	if refreshed.Changed || refreshed.Generation != created.Generation {
		t.Fatalf("same projection rotated generation: before=%#v after=%#v", created, refreshed)
	}
	var validUntil time.Time
	if err := pool.QueryRow(ctx, `SELECT valid_until FROM youtube_collection_targets WHERE projection_generation = $1`, created.Generation).Scan(&validUntil); err != nil {
		t.Fatal(err)
	}
	if !validUntil.Equal(projectionNow.Add(70 * time.Minute)) {
		t.Fatalf("valid_until = %s", validUntil)
	}

	newReason := reason
	newReason.ReasonKey = "room:b"
	reasonOnly := mustRefresh(t, ctx, refresher, staticBuilder{targets: []TargetSpec{first}, reasons: []TargetReason{newReason}}, projectionNow.Add(20*time.Minute), "reason-only")
	if reasonOnly.Changed || reasonOnly.Generation != created.Generation {
		t.Fatalf("reason-only refresh rotated generation: %#v", reasonOnly)
	}
	var reasonKey string
	if err := pool.QueryRow(ctx, `SELECT reason_key FROM youtube_collection_target_reasons WHERE projection_generation = $1`, created.Generation).Scan(&reasonKey); err != nil {
		t.Fatal(err)
	}
	if reasonKey != "room:b" {
		t.Fatalf("reason key = %q", reasonKey)
	}

	changedTarget := first
	changedTarget.PollInterval = 2 * time.Minute
	changed := mustRefresh(t, ctx, refresher, staticBuilder{targets: []TargetSpec{changedTarget}, reasons: []TargetReason{newReason}}, projectionNow.Add(30*time.Minute), "changed")
	if !changed.Changed || changed.Generation == created.Generation {
		t.Fatalf("changed result = %#v", changed)
	}
	assertGenerationStatus(t, pool, created.Generation, "RETIRED")
	assertGenerationStatus(t, pool, changed.Generation, "CURRENT")

	empty := mustRefresh(t, ctx, refresher, staticBuilder{}, projectionNow.Add(40*time.Minute), "empty")
	if !empty.Changed || empty.RowCount != 0 || empty.Generation == changed.Generation {
		t.Fatalf("empty result = %#v", empty)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM youtube_collection_targets WHERE projection_generation = $1`, empty.Generation).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("empty generation targets = %d", count)
	}
}

func mustRefresh(t *testing.T, ctx context.Context, refresher *Refresher, builder Builder, now time.Time, label string) Result {
	t.Helper()
	result, err := refresher.Refresh(ctx, builder, now)
	if err != nil {
		t.Fatalf("%s refresh: %v", label, err)
	}
	return result
}

func TestRetainDeletesOnlyUnlockedRetiredProjectionState(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	refresher, err := NewRefresher(pool, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	target := TargetSpec{SubjectKey: "channel:a", ObservationKind: contract.KindCommunityPage, Priority: 50, PollInterval: time.Minute, Enabled: true}
	first, err := refresher.Refresh(ctx, staticBuilder{targets: []TargetSpec{target}}, projectionNow)
	if err != nil {
		t.Fatal(err)
	}
	target.PollInterval = 2 * time.Minute
	second, err := refresher.Refresh(ctx, staticBuilder{targets: []TargetSpec{target}}, projectionNow.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	target.PollInterval = 3 * time.Minute
	current, err := refresher.Refresh(ctx, staticBuilder{targets: []TargetSpec{target}}, projectionNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	insertProjectionLease(t, pool, "retired-idle", first.Generation, "IDLE", projectionNow.Add(7*24*time.Hour))
	insertProjectionLease(t, pool, "retired-active", second.Generation, "ACTIVE", projectionNow.Add(7*24*time.Hour))

	result, err := refresher.Retain(ctx, projectionNow.Add(4*24*time.Hour), 24*time.Hour, 100)
	if err != nil {
		t.Fatal(err)
	}
	if result.LeasesDeleted != 1 || result.GenerationsDeleted != 1 {
		t.Fatalf("retention result = %#v", result)
	}
	assertGenerationMissing(t, pool, first.Generation)
	assertGenerationStatus(t, pool, second.Generation, "RETIRED")
	assertGenerationStatus(t, pool, current.Generation, "CURRENT")
}

func insertProjectionLease(t *testing.T, pool *pgxpool.Pool, key string, generation int64, state string, expiresAt time.Time) {
	t.Helper()
	var owner any
	var leaseExpires any
	if state == "ACTIVE" {
		owner = "collector-a"
		leaseExpires = expiresAt
	}
	_, err := pool.Exec(context.Background(), `
		INSERT INTO youtube_collection_job_leases (
			job_key, provider, job_class, collection_job_kind, subject_key,
			projection_generation, poll_interval_ms, slot_state, scheduled_for,
			next_due_at, owner_instance, lease_expires_at
		) VALUES ($1, 'youtubejs', 'SUBJECT', 'youtubejs_community', 'channel:a', $2, 60000, $3, $4, $4, $5, $6)
	`, key, generation, state, projectionNow, owner, leaseExpires)
	if err != nil {
		t.Fatal(err)
	}
}

func assertGenerationMissing(t *testing.T, pool *pgxpool.Pool, generation int64) {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM youtube_collection_projection_generations WHERE generation = $1`, generation).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("generation %d still exists", generation)
	}
}

func TestRefreshInputFailurePreservesCurrent(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.NewPool(t)
	refresher, err := NewRefresher(pool, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	target := TargetSpec{SubjectKey: "channel:a", ObservationKind: contract.KindVideoList, Priority: 40, PollInterval: time.Minute, Enabled: true}
	current, err := refresher.Refresh(ctx, staticBuilder{targets: []TargetSpec{target}}, projectionNow)
	if err != nil {
		t.Fatal(err)
	}
	_, err = refresher.Refresh(ctx, staticBuilder{err: errors.New("input unavailable")}, projectionNow.Add(time.Minute))
	if !errors.Is(err, ErrInputRead) && (err == nil || !strings.Contains(err.Error(), "input unavailable")) {
		t.Fatalf("refresh error = %v", err)
	}
	assertGenerationStatus(t, pool, current.Generation, "CURRENT")
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM youtube_collection_targets WHERE projection_generation = $1`, current.Generation).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("preserved target count = %d", count)
	}
}

func TestRefreshRejectsConflictingAndUnboundedTargetsBeforeActivation(t *testing.T) {
	pool := dbtest.NewPool(t)
	refresher, err := NewRefresher(pool, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	first := TargetSpec{SubjectKey: "channel:a", ObservationKind: contract.KindCommunityPage, Priority: 50, PollInterval: time.Minute, Enabled: true}
	conflict := first
	conflict.Priority = 51
	if _, err := refresher.Refresh(context.Background(), staticBuilder{targets: []TargetSpec{first, conflict}}, projectionNow); !errors.Is(err, ErrInvalidProjection) {
		t.Fatalf("conflicting targets error = %v", err)
	}
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM youtube_collection_projection_generations`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("generation count after invalid refresh = %d", count)
	}

	tooMany := make([]TargetSpec, MaxTargetCount+1)
	if _, err := refresher.Refresh(context.Background(), staticBuilder{targets: tooMany}, projectionNow); !errors.Is(err, ErrInvalidProjection) {
		t.Fatalf("unbounded targets error = %v", err)
	}
}

func TestNormalizeProjectionIsOrderIndependent(t *testing.T) {
	left := []TargetSpec{
		{SubjectKey: "channel:b", ObservationKind: contract.KindVideoList, Priority: 10, PollInterval: time.Minute, Enabled: true},
		{SubjectKey: "channel:a", ObservationKind: contract.KindCommunityPage, Priority: 20, PollInterval: 2 * time.Minute, Enabled: false},
	}
	right := []TargetSpec{left[1], left[0]}
	_, _, leftHash, err := normalize(left, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _, rightHash, err := normalize(right, nil)
	if err != nil {
		t.Fatal(err)
	}
	if leftHash != rightHash {
		t.Fatalf("projection hashes differ: %s != %s", leftHash, rightHash)
	}
}

func TestBuildPolicyTargetsMaintainsSourceMapping(t *testing.T) {
	schedules := defaultPolicySchedules()
	targets, reasons, err := BuildPolicyTargets(PolicyInputs{
		NotificationChannelIDs: []string{"channel:notify"},
		OperationalChannelIDs:  []string{"channel:ops"},
		ViewerVideoIDs:         []string{"vid-live-1"},
	}, schedules)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 9 || len(reasons) != 9 {
		t.Fatalf("targets/reasons = %d/%d", len(targets), len(reasons))
	}
	want := map[string]bool{
		"channel:notify/community_page":              true,
		"channel:notify/video_list":                  true,
		"channel:notify/shorts_list":                 true,
		"channel:ops/live_snapshot":                  true,
		"vid-live-1/viewer_sample":                   true,
		"channel:ops/channel_stats":                  true,
		"channel:ops/channel_profile":                true,
		"channel:ops/channel_photo":                  true,
		"global:hololive-schedule/schedule_snapshot": true,
	}
	for _, target := range targets {
		delete(want, target.SubjectKey+"/"+string(target.ObservationKind))
	}
	if len(want) != 0 {
		t.Fatalf("missing mapped targets: %#v", want)
	}
}

func TestBuildPolicyTargetsDoesNotPlantViewerSampleOnChannelIDs(t *testing.T) {
	targets, _, err := BuildPolicyTargets(PolicyInputs{
		OperationalChannelIDs: []string{"UCoperationalchannel0001"},
		ViewerVideoIDs:        []string{"vid-live-1"},
	}, defaultPolicySchedules())
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.ObservationKind != contract.KindViewerSample {
			continue
		}
		if target.SubjectKey != "vid-live-1" {
			t.Fatalf("viewer_sample subject = %q, want video id", target.SubjectKey)
		}
	}
}

func TestBuildPolicyTargetsEmptyViewerRosterSucceeds(t *testing.T) {
	targets, _, err := BuildPolicyTargets(PolicyInputs{
		NotificationChannelIDs: []string{"UC_NOTIFY"},
		OperationalChannelIDs:  []string{"UC_OPS"},
	}, defaultPolicySchedules())
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.ObservationKind == contract.KindViewerSample {
			t.Fatalf("empty viewer roster planted viewer_sample on %q", target.SubjectKey)
		}
	}
}

func TestBuildPolicyTargetsRejectsChannelIDAsViewerVideo(t *testing.T) {
	_, _, err := BuildPolicyTargets(PolicyInputs{
		ViewerVideoIDs: []string{"UCoperationalchannel0001"},
	}, defaultPolicySchedules())
	if !errors.Is(err, ErrInvalidProjection) {
		t.Fatalf("error = %v, want invalid projection", err)
	}
}

func defaultPolicySchedules() map[contract.ObservationKind]Schedule {
	schedules := make(map[contract.ObservationKind]Schedule)
	for _, kind := range []contract.ObservationKind{
		contract.KindCommunityPage, contract.KindVideoList, contract.KindShortsList,
		contract.KindLiveSnapshot, contract.KindViewerSample, contract.KindChannelStats,
		contract.KindChannelProfile, contract.KindChannelPhoto, contract.KindSchedule,
	} {
		schedules[kind] = Schedule{Priority: 50, PollInterval: time.Minute, Enabled: true}
	}
	return schedules
}

func assertGenerationStatus(t *testing.T, pool *pgxpool.Pool, generation int64, want string) {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM youtube_collection_projection_generations WHERE generation = $1`, generation).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != want {
		t.Fatalf("generation %d status = %q, want %q", generation, status, want)
	}
}
