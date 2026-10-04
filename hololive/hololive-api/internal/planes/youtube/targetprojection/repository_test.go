package targetprojection

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

const (
	subjectChannelA          = "channel:a"
	testOperationalChannelID = "UC_OPS"
)

var projectionNow = time.Date(2026, time.August, 14, 2, 0, 0, 0, time.UTC)

type staticBuilder struct {
	targets []TargetSpec
	reasons []TargetReason
	err     error
}

func (b staticBuilder) Build(context.Context, dbx.Tx, time.Time) ([]TargetSpec, []TargetReason, error) {
	return b.targets, b.reasons, b.err
}

func TestRefreshProjectionPaths(t *testing.T) {
	pool := dbtest.NewPool(t)

	refresher, err := NewRefresher(pool, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	first := TargetSpec{SubjectKey: subjectChannelA, ObservationKind: contract.KindCommunityPage, Priority: 50, PollInterval: time.Minute, Enabled: true}
	reason := TargetReason{SubjectKey: first.SubjectKey, ObservationKind: first.ObservationKind, ReasonKind: "notification_target", ReasonKey: "room:a"}

	created := mustRefresh(t, refresher, staticBuilder{targets: []TargetSpec{first}, reasons: []TargetReason{reason}}, projectionNow, "initial")
	if !created.Changed || created.Generation <= 0 || created.RowCount != 1 {
		t.Fatalf("initial result = %#v", created)
	}

	refreshed := mustRefresh(t, refresher, staticBuilder{targets: []TargetSpec{first, first}, reasons: []TargetReason{reason, reason}}, projectionNow.Add(10*time.Minute), "same projection")
	if refreshed.Changed || refreshed.Generation != created.Generation {
		t.Fatalf("same projection rotated generation: before=%#v after=%#v", created, refreshed)
	}

	assertGenerationValidUntil(t, pool, created.Generation, projectionNow.Add(70*time.Minute))

	newReason := reason

	newReason.ReasonKey = "room:b"

	reasonOnly := mustRefresh(t, refresher, staticBuilder{targets: []TargetSpec{first}, reasons: []TargetReason{newReason}}, projectionNow.Add(20*time.Minute), "reason-only")

	if reasonOnly.Changed || reasonOnly.Generation != created.Generation {
		t.Fatalf("reason-only refresh rotated generation: %#v", reasonOnly)
	}

	assertReasonKey(t, pool, created.Generation, "room:b")

	changedTarget := first

	changedTarget.PollInterval = 2 * time.Minute

	changed := mustRefresh(t, refresher, staticBuilder{targets: []TargetSpec{changedTarget}, reasons: []TargetReason{newReason}}, projectionNow.Add(30*time.Minute), "changed")

	if !changed.Changed || changed.Generation == created.Generation {
		t.Fatalf("changed result = %#v", changed)
	}

	assertGenerationStatus(t, pool, created.Generation, "RETIRED")
	assertGenerationStatus(t, pool, changed.Generation, "CURRENT")

	assertEmptyProjectionRefresh(t, pool, refresher, changed.Generation)
}

func assertEmptyProjectionRefresh(t *testing.T, pool *pgxpool.Pool, refresher *Refresher, previous int64) {
	t.Helper()

	empty := mustRefresh(t, refresher, staticBuilder{}, projectionNow.Add(40*time.Minute), "empty")
	if !empty.Changed || empty.RowCount != 0 || empty.Generation == previous {
		t.Fatalf("empty result = %#v", empty)
	}

	assertTargetCount(t, pool, empty.Generation, 0)
	requireEligibilityVersion(t, pool, empty.Generation, 1)

	emptyHeartbeat := mustRefresh(t, refresher, staticBuilder{}, projectionNow.Add(50*time.Minute), "empty heartbeat")
	if emptyHeartbeat.Changed || emptyHeartbeat.Generation != empty.Generation {
		t.Fatalf("empty heartbeat rotated generation: %#v", emptyHeartbeat)
	}

	requireEligibilityVersion(t, pool, empty.Generation, 1)
	assertGenerationValidUntil(t, pool, empty.Generation, projectionNow.Add(110*time.Minute))
}

func TestRefreshProjectionUsesCanonicalByteOrderAcrossDatabaseCollations(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	refresher, err := NewRefresher(pool, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	targets := []TargetSpec{
		{SubjectKey: "global:hololive-schedule", ObservationKind: contract.KindSchedule, Priority: 30, PollInterval: 5 * time.Minute, Enabled: true},
		{SubjectKey: "UCJFZiqLMntJufDCHc6bQixg", ObservationKind: contract.KindCommunityPage, Priority: 40, PollInterval: 2 * time.Minute, Enabled: true},
	}
	reasons := []TargetReason{
		{SubjectKey: targets[0].SubjectKey, ObservationKind: targets[0].ObservationKind, ReasonKind: "fixed_global", ReasonKey: targets[0].SubjectKey},
		{SubjectKey: targets[1].SubjectKey, ObservationKind: targets[1].ObservationKind, ReasonKind: "notification_target", ReasonKey: targets[1].SubjectKey},
	}

	result, err := refresher.Refresh(ctx, staticBuilder{targets: targets, reasons: reasons}, projectionNow)
	if err != nil {
		t.Fatal(err)
	}

	if !result.Changed || result.RowCount != 2 {
		t.Fatalf("refresh result = %#v", result)
	}
}

func TestProjectionVerificationQueriesUseCanonicalByteOrder(t *testing.T) {
	for _, name := range []string{"load_targets.sql", "load_reasons.sql"} {
		query := mustSQL(name)
		if !strings.Contains(query, `subject_key COLLATE "C"`) ||
			!strings.Contains(query, `observation_kind COLLATE "C"`) {
			t.Fatalf("%s must use canonical byte ordering", name)
		}
	}

	reasonsQuery := mustSQL("load_reasons.sql")
	if !strings.Contains(reasonsQuery, `reason_kind COLLATE "C"`) ||
		!strings.Contains(reasonsQuery, `reason_key COLLATE "C"`) {
		t.Fatal("load_reasons.sql must canonically order reason identity")
	}
}

func mustRefresh(t *testing.T, refresher *Refresher, builder Builder, now time.Time, label string) Result {
	t.Helper()

	result, err := refresher.Refresh(t.Context(), builder, now)
	if err != nil {
		t.Fatalf("%s refresh: %v", label, err)
	}

	return result
}

func TestRetainDeletesOnlyUnlockedRetiredProjectionState(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	refresher, err := NewRefresher(pool, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	target := TargetSpec{SubjectKey: subjectChannelA, ObservationKind: contract.KindCommunityPage, Priority: 50, PollInterval: time.Minute, Enabled: true}

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
	// 삭제 함수는 lease 만료만 clock_timestamp()로 판정하므로, 고정 anchor 기준으로 잡으면
	// 실제 시각이 anchor+7일을 넘긴 날부터 이 lease가 만료로 보여 테스트가 깨진다.
	insertProjectionLease(t, pool, "retired-active", second.Generation, "ACTIVE", time.Now().Add(7*24*time.Hour))

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

func TestRetiredLeaseRetentionUsesRestrictedFunction(t *testing.T) {
	query := mustSQL("delete_retired_job_leases.sql")
	if !strings.Contains(query, "delete_retired_youtube_collection_job_leases") || strings.Contains(query, "FOR UPDATE") {
		t.Fatal("retired lease retention must use the restricted retention function")
	}
}

func insertProjectionLease(t *testing.T, pool *pgxpool.Pool, key string, generation int64, state string, expiresAt time.Time) {
	t.Helper()

	var (
		owner        any
		leaseExpires any
	)

	if state == "ACTIVE" {
		owner = "collector-a"
		leaseExpires = expiresAt
	}

	_, err := pool.Exec(t.Context(), `
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

	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_collection_projection_generations WHERE generation = $1`, generation).Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatalf("generation %d still exists", generation)
	}
}

func TestRefreshInputFailurePreservesCurrent(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)

	refresher, err := NewRefresher(pool, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	target := TargetSpec{SubjectKey: subjectChannelA, ObservationKind: contract.KindVideoList, Priority: 40, PollInterval: time.Minute, Enabled: true}

	current, err := refresher.Refresh(ctx, staticBuilder{targets: []TargetSpec{target}}, projectionNow)
	if err != nil {
		t.Fatal(err)
	}

	_, err = refresher.Refresh(ctx, staticBuilder{err: errors.New("input unavailable")}, projectionNow.Add(time.Minute))
	if !errors.Is(err, ErrInputRead) && (err == nil || !strings.Contains(err.Error(), "input unavailable")) {
		t.Fatalf("refresh error = %v", err)
	}

	assertGenerationStatus(t, pool, current.Generation, "CURRENT")

	assertTargetCount(t, pool, current.Generation, 1)
}

func TestRefreshRejectsConflictingAndUnboundedTargetsBeforeActivation(t *testing.T) {
	pool := dbtest.NewPool(t)

	refresher, err := NewRefresher(pool, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	first := TargetSpec{SubjectKey: subjectChannelA, ObservationKind: contract.KindCommunityPage, Priority: 50, PollInterval: time.Minute, Enabled: true}
	conflict := first

	conflict.Priority = 51

	if _, err := refresher.Refresh(t.Context(), staticBuilder{targets: []TargetSpec{first, conflict}}, projectionNow); !errors.Is(err, ErrInvalidProjection) {
		t.Fatalf("conflicting targets error = %v", err)
	}

	var count int

	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_collection_projection_generations`).Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatalf("generation count after invalid refresh = %d", count)
	}

	tooMany := make([]TargetSpec, MaxTargetCount+1)
	if _, err := refresher.Refresh(t.Context(), staticBuilder{targets: tooMany}, projectionNow); !errors.Is(err, ErrInvalidProjection) {
		t.Fatalf("unbounded targets error = %v", err)
	}
}

func TestNormalizeProjectionIsOrderIndependent(t *testing.T) {
	left := []TargetSpec{
		{SubjectKey: "channel:b", ObservationKind: contract.KindVideoList, Priority: 10, PollInterval: time.Minute, Enabled: true},
		{SubjectKey: subjectChannelA, ObservationKind: contract.KindCommunityPage, Priority: 20, PollInterval: 2 * time.Minute, Enabled: false},
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
		LiveCheckVideos:        []LiveCheckVideo{{VideoID: "video-stale", ChannelID: "channel:ops"}},
	}, schedules)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"channel:notify/community_page":              true,
		"channel:notify/video_list":                  true,
		"channel:notify/shorts_list":                 true,
		"channel:ops/live_snapshot":                  true,
		"channel:ops/channel_live_check":             true,
		"channel:ops/channel_profile":                true,
		"channel:ops/channel_photo":                  true,
		"video-stale/video_live_check":               true,
		"global:hololive-schedule/schedule_snapshot": true,
	}

	for _, target := range targets {
		key := target.SubjectKey + "/" + string(target.ObservationKind)
		if !want[key] {
			t.Fatalf("unexpected mapped target %s", key)
		}

		delete(want, key)
	}

	if len(want) != 0 {
		t.Fatalf("missing mapped targets: %#v", want)
	}

	for _, reason := range reasons {
		if reason.ObservationKind == contract.KindVideoLiveCheck &&
			(reason.SubjectKey != "video-stale" || reason.ReasonKind != "live_session_check" || reason.ReasonKey != "channel:ops") {
			t.Fatalf("live check video reason = %+v", reason)
		}
	}
}

func TestDefaultLiveChecksShareLiveSnapshotCadence(t *testing.T) {
	targets, _, err := BuildPolicyTargets(PolicyInputs{
		OperationalChannelIDs: []string{testOperationalChannelID},
		LiveCheckVideos:       []LiveCheckVideo{{VideoID: "vid-stale", ChannelID: testOperationalChannelID}},
	}, DefaultPolicySchedules())
	if err != nil {
		t.Fatal(err)
	}

	byKind := make(map[contract.ObservationKind]TargetSpec, len(targets))
	for _, target := range targets {
		byKind[target.ObservationKind] = target
	}

	live := byKind[contract.KindLiveSnapshot]
	if live.PollInterval != 2*time.Minute || live.Priority != 20 || !live.Enabled {
		t.Fatalf("live snapshot schedule = %+v", live)
	}

	// 독립 job들도 동일한 기본 재확인 주기와 freshness 예산을 사용한다.
	for _, kind := range []contract.ObservationKind{contract.KindChannelLiveCheck, contract.KindVideoLiveCheck} {
		got := byKind[kind]
		if got.PollInterval != live.PollInterval || got.Priority != live.Priority || got.Enabled != live.Enabled {
			t.Fatalf("%s schedule = %+v, want live snapshot cadence %+v", kind, got, live)
		}
	}

	if got := byKind[contract.KindVideoLiveCheck].SubjectKey; got != "vid-stale" {
		t.Fatalf("video live check subject = %q, want video ID", got)
	}
}

func TestLiveFreshnessBudgetMatchesLiveQueryBound(t *testing.T) {
	for _, tc := range []struct {
		poll time.Duration
		want time.Duration
	}{
		{poll: 2 * time.Minute, want: 270 * time.Second},
		{poll: 135 * time.Second, want: 5 * time.Minute},
		{poll: 10 * time.Minute, want: 5 * time.Minute},
		{poll: time.Second, want: 32 * time.Second},
	} {
		if got := LiveFreshnessBudget(tc.poll); got != tc.want {
			t.Fatalf("LiveFreshnessBudget(%s) = %s, want %s", tc.poll, got, tc.want)
		}
	}
}

func TestBuildPolicyTargetsRejectsInvalidLiveCheckVideos(t *testing.T) {
	overflow := make([]LiveCheckVideo, MaxInputLiveCheckVideoCount+1)
	for i := range overflow {
		overflow[i] = LiveCheckVideo{VideoID: fmt.Sprintf("vid-%d", i), ChannelID: testOperationalChannelID}
	}

	for name, videos := range map[string][]LiveCheckVideo{
		"outside roster": {{VideoID: "vid-a", ChannelID: "UC_OTHER"}},
		"empty video":    {{VideoID: " ", ChannelID: testOperationalChannelID}},
		"empty channel":  {{VideoID: "vid-a", ChannelID: ""}},
		"overflow":       overflow,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := BuildPolicyTargets(PolicyInputs{
				OperationalChannelIDs: []string{testOperationalChannelID},
				LiveCheckVideos:       videos,
			}, DefaultPolicySchedules())
			if !errors.Is(err, ErrInvalidProjection) {
				t.Fatalf("BuildPolicyTargets() error = %v, want invalid projection", err)
			}
		})
	}

	// 상한과 같은 후보 수는 유효한 projection이다.
	atLimit := overflow[:MaxInputLiveCheckVideoCount]
	if _, _, err := BuildPolicyTargets(PolicyInputs{
		OperationalChannelIDs: []string{testOperationalChannelID},
		LiveCheckVideos:       atLimit,
	}, DefaultPolicySchedules()); err != nil {
		t.Fatalf("live check videos at limit: %v", err)
	}
}

type recordingInputReader struct {
	operational []string
	videos      []LiveCheckVideo
	videosErr   error
	query       *LiveCheckVideoQuery
}

func (recordingInputReader) NotificationChannelIDs(context.Context, dbx.Tx) ([]string, error) {
	return nil, nil
}

func (r recordingInputReader) OperationalChannelIDs(context.Context, dbx.Tx) ([]string, error) {
	return r.operational, nil
}

func (r recordingInputReader) LiveCheckVideos(_ context.Context, _ dbx.Tx, query LiveCheckVideoQuery) ([]LiveCheckVideo, error) {
	*r.query = query

	return r.videos, r.videosErr
}

func TestPolicyBuilderQueriesLiveCheckVideosWithRosterAndBudget(t *testing.T) {
	var query LiveCheckVideoQuery

	builder := PolicyBuilder{
		Reader: recordingInputReader{
			operational: []string{testOperationalChannelID},
			videos:      []LiveCheckVideo{{VideoID: "vid-stale", ChannelID: testOperationalChannelID, NotBefore: projectionNow}},
			query:       &query,
		},
		Schedules: DefaultPolicySchedules(),
	}

	targets, _, err := builder.Build(t.Context(), nil, projectionNow)
	if err != nil {
		t.Fatal(err)
	}

	if query.FreshnessBudget != 270*time.Second ||
		len(query.OperationalChannelIDs) != 1 || query.OperationalChannelIDs[0] != testOperationalChannelID {
		t.Fatalf("live check video query = %+v", query)
	}

	if !slices.ContainsFunc(targets, func(target TargetSpec) bool {
		return target.SubjectKey == "vid-stale" && target.ObservationKind == contract.KindVideoLiveCheck &&
			target.NotBefore.Equal(projectionNow)
	}) {
		t.Fatalf("live check video target with not_before missing: %+v", targets)
	}
}

func TestPolicyBuilderLiveCheckVideoFailuresPreserveLastGood(t *testing.T) {
	for name, videosErr := range map[string]error{
		"read failure": errors.New("live check videos unavailable"),
		"overflow":     fmt.Errorf("%w: live check video count exceeds %d", ErrInvalidProjection, MaxInputLiveCheckVideoCount),
	} {
		t.Run(name, func(t *testing.T) {
			var query LiveCheckVideoQuery

			builder := PolicyBuilder{
				Reader:    recordingInputReader{operational: []string{testOperationalChannelID}, videosErr: videosErr, query: &query},
				Schedules: DefaultPolicySchedules(),
			}

			// 입력 오류는 runtime이 degraded로 기록하고 last-good generation을 유지하는 ErrInputRead로 분류된다.
			if _, _, err := builder.Build(t.Context(), nil, projectionNow); !errors.Is(err, ErrInputRead) || !errors.Is(err, videosErr) {
				t.Fatalf("Build() error = %v, want input read wrapping %v", err, videosErr)
			}
		})
	}

	schedules := DefaultPolicySchedules()
	delete(schedules, contract.KindLiveSnapshot)

	var query LiveCheckVideoQuery

	builder := PolicyBuilder{Reader: recordingInputReader{query: &query}, Schedules: schedules}
	if _, _, err := builder.Build(t.Context(), nil, projectionNow); !errors.Is(err, ErrInvalidProjection) {
		t.Fatalf("missing live snapshot schedule error = %v", err)
	}
}

func TestBuildPolicyTargetsDoesNotCollectViewers(t *testing.T) {
	targets, _, err := BuildPolicyTargets(PolicyInputs{
		NotificationChannelIDs: []string{"UC_NOTIFY"},
		OperationalChannelIDs:  []string{testOperationalChannelID},
	}, defaultPolicySchedules())
	if err != nil {
		t.Fatal(err)
	}

	for _, target := range targets {
		if target.ObservationKind == contract.KindViewerSample {
			t.Fatalf("policy planted retired viewer_sample on %q", target.SubjectKey)
		}
	}
}

func TestRefreshRetiresViewerTargetsWithoutDeletingHistoricalGeneration(t *testing.T) {
	pool := dbtest.NewPool(t)

	refresher, err := NewRefresher(pool, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	targets, reasons, err := BuildPolicyTargets(PolicyInputs{
		NotificationChannelIDs: []string{"channel:notify"},
		OperationalChannelIDs:  []string{"channel:ops"},
	}, DefaultPolicySchedules())
	if err != nil {
		t.Fatal(err)
	}

	historicalViewer := TargetSpec{
		SubjectKey: "vid-live-1", ObservationKind: contract.KindViewerSample,
		Priority: 20, PollInterval: 2 * time.Minute, Enabled: true,
	}
	previous := mustRefresh(t, refresher, staticBuilder{
		targets: append(targets, historicalViewer),
		reasons: append(reasons, TargetReason{
			SubjectKey: historicalViewer.SubjectKey, ObservationKind: historicalViewer.ObservationKind,
			ReasonKind: "viewer_roster", ReasonKey: historicalViewer.SubjectKey,
		}),
	}, projectionNow, "historical viewer projection")
	current := mustRefresh(t, refresher, staticBuilder{targets: targets, reasons: reasons},
		projectionNow.Add(time.Minute), "viewer retirement")

	if !current.Changed || current.Generation == previous.Generation || current.RowCount != len(targets) {
		t.Fatalf("retirement did not rotate projection: previous=%+v current=%+v", previous, current)
	}

	assertGenerationStatus(t, pool, previous.Generation, "RETIRED")
	assertGenerationStatus(t, pool, current.Generation, "CURRENT")
	assertTargetCount(t, pool, previous.Generation, len(targets)+1)
	assertTargetCount(t, pool, current.Generation, len(targets))

	var historicalViewers, currentViewers int

	if err := pool.QueryRow(t.Context(), `
		SELECT COUNT(*) FILTER (WHERE projection_generation = $1),
		       COUNT(*) FILTER (WHERE projection_generation = $2)
		FROM youtube_collection_targets
		WHERE observation_kind = 'viewer_sample'
	`, previous.Generation, current.Generation).Scan(&historicalViewers, &currentViewers); err != nil {
		t.Fatal(err)
	}

	if historicalViewers != 1 || currentViewers != 0 {
		t.Fatalf("viewer targets: historical=%d current=%d", historicalViewers, currentViewers)
	}

	refreshed := mustRefresh(t, refresher, staticBuilder{targets: targets, reasons: reasons},
		projectionNow.Add(2*time.Minute), "unchanged retired policy")
	if refreshed.Changed || refreshed.Generation != current.Generation {
		t.Fatalf("unchanged policy rotated generation: %+v", refreshed)
	}
}

func defaultPolicySchedules() map[contract.ObservationKind]Schedule {
	schedules := make(map[contract.ObservationKind]Schedule)

	for _, kind := range []contract.ObservationKind{
		contract.KindCommunityPage, contract.KindVideoList, contract.KindShortsList,
		contract.KindLiveSnapshot, contract.KindChannelLiveCheck, contract.KindVideoLiveCheck,
		contract.KindChannelProfile, contract.KindChannelPhoto, contract.KindSchedule,
	} {
		schedules[kind] = Schedule{Priority: 50, PollInterval: time.Minute, Enabled: true}
	}

	return schedules
}

func assertGenerationValidUntil(t *testing.T, pool *pgxpool.Pool, generation int64, want time.Time) {
	t.Helper()

	var validUntil time.Time

	if err := pool.QueryRow(t.Context(), `SELECT valid_until FROM youtube_collection_projection_generations WHERE generation = $1`, generation).Scan(&validUntil); err != nil {
		t.Fatal(err)
	}

	if !validUntil.Equal(want) {
		t.Fatalf("generation %d valid_until = %s, want %s", generation, validUntil, want)
	}
}

func assertReasonKey(t *testing.T, pool *pgxpool.Pool, generation int64, want string) {
	t.Helper()

	var reasonKey string

	if err := pool.QueryRow(t.Context(), `SELECT reason_key FROM youtube_collection_target_reasons WHERE projection_generation = $1`, generation).Scan(&reasonKey); err != nil {
		t.Fatal(err)
	}

	if reasonKey != want {
		t.Fatalf("generation %d reason key = %q, want %q", generation, reasonKey, want)
	}
}

func assertTargetCount(t *testing.T, pool *pgxpool.Pool, generation int64, want int) {
	t.Helper()

	var count int

	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM youtube_collection_targets WHERE projection_generation = $1`, generation).Scan(&count); err != nil {
		t.Fatal(err)
	}

	if count != want {
		t.Fatalf("generation %d target count = %d, want %d", generation, count, want)
	}
}

func assertGenerationStatus(t *testing.T, pool *pgxpool.Pool, generation int64, want string) {
	t.Helper()

	var status string

	if err := pool.QueryRow(t.Context(), `SELECT status FROM youtube_collection_projection_generations WHERE generation = $1`, generation).Scan(&status); err != nil {
		t.Fatal(err)
	}

	if status != want {
		t.Fatalf("generation %d status = %q, want %q", generation, status, want)
	}
}
