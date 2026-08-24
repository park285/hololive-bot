package collectorruntime

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

var readinessHTTPStatusTable = map[ReadinessState]struct {
	status     int
	ready      string
	dependency bool
}{
	ReadyStarting:          {status: http.StatusServiceUnavailable, ready: statusNotReady, dependency: true},
	ReadyWaitingCollection: {status: http.StatusServiceUnavailable, ready: statusNotReady, dependency: true},
	ReadyWaitingHandoff:    {status: http.StatusServiceUnavailable, ready: statusNotReady, dependency: true},
	ReadyDegraded:          {status: http.StatusServiceUnavailable, ready: statusNotReady, dependency: true},
	ReadyReady:             {status: http.StatusOK, ready: "ready", dependency: false},
}

func TestRDY001NilDependenciesAreDegradedWithExactName(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	tests := []struct {
		name       string
		mutate     func(*readinessDeps)
		dependency string
	}{
		{name: "scheduler", mutate: func(d *readinessDeps) { d.scheduler = nil }, dependency: "scheduler"},
		{name: "helper", mutate: func(d *readinessDeps) { d.helper = nil }, dependency: "youtubejs"},
		{name: "postgres", mutate: func(d *readinessDeps) { d.store = nil }, dependency: "postgres_queue"},
		{name: "tracker", mutate: func(d *readinessDeps) { d.tracker = nil }, dependency: "first_success"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			deps := readyDeps()
			test.mutate(&deps)

			body := evaluateReadiness(ctx, &deps)
			if body.Status != statusNotReady || body.State != ReadyDegraded || body.Dependency != test.dependency {
				t.Fatalf("body = %+v, want DEGRADED %s", body, test.dependency)
			}

			if readinessHTTPStatus(&body) != http.StatusServiceUnavailable {
				t.Fatalf("http = %d", readinessHTTPStatus(&body))
			}
		})
	}
}

func TestRDY002HelperHealthTimeoutFailsInsideTotalBudget(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 80*time.Millisecond)

	defer cancel()

	deps := readyDeps()

	deps.helperTimeout = 40 * time.Millisecond
	deps.helper = &stubHelper{health: func(callCtx context.Context) error {
		<-callCtx.Done()

		return callCtx.Err()
	}}

	started := time.Now()
	body := evaluateReadiness(ctx, &deps)
	elapsed := time.Since(started)

	if elapsed > 80*time.Millisecond+30*time.Millisecond {
		t.Fatalf("elapsed = %s, exceeded total readiness budget", elapsed)
	}

	if body.Status != statusNotReady || body.Dependency != "youtubejs" || readinessHTTPStatus(&body) != http.StatusServiceUnavailable {
		t.Fatalf("timeout body = %+v", body)
	}
}

func TestRDY003PendingCountUsesCapSentinel(t *testing.T) {
	t.Parallel()

	got, err := newBoundedCount(pendingQueueCap+1, pendingQueueCap)
	if err != nil || got.Value != pendingQueueCap || !got.Capped {
		t.Fatalf("bounded count = %+v, %v", got, err)
	}

	sql := mustSQL("pending_observation_count.sql")
	if !strings.Contains(sql, "LIMIT $1 + 1") {
		t.Fatal("pending count must use cap+1 sentinel")
	}

	handoffSQL := mustSQL("observation_handoff_status.sql")
	if !strings.Contains(handoffSQL, "unnest($1::bigint[]) WITH ORDINALITY") {
		t.Fatal("handoff status query must keep requested ID order")
	}

	if strings.Contains(sql, "COUNT(observation_id)") {
		t.Fatal("unbounded pending COUNT path must be removed")
	}

	if _, err := newBoundedCount(pendingQueueCap+2, pendingQueueCap); err == nil {
		t.Fatal("count above cap+1 must fail closed")
	}

	if _, err := (&postgresQueueStore{}).CountPending(t.Context(), pendingQueueCap); err == nil {
		t.Fatal("nil postgres pool must fail closed")
	}
}

func TestRDY004NoDataTerminalWaitsForHandoff(t *testing.T) {
	t.Parallel()

	scheduler := &leaseScheduler{readiness: &readinessTracker{}}
	scheduler.recordTerminalSuccess(nil)

	deps := readyDeps()

	deps.tracker = scheduler.readiness

	body := evaluateReadiness(t.Context(), &deps)

	if !body.FirstSuccess || body.State != ReadyWaitingHandoff || body.Dependency != "observation_handoff" || body.HandoffCandidates != 0 {
		t.Fatalf("no-data body = %+v", body)
	}
}

func TestRDY005CollisionOnlyTerminalHasZeroCandidates(t *testing.T) {
	t.Parallel()

	scheduler := &leaseScheduler{readiness: &readinessTracker{}}
	published := sourceobservation.PublishBatchResult{Results: []sourceobservation.PublishedObservation{
		sourceobservation.NewPublishedObservation(11, sourceobservation.PublishCollision, 0),
	}}
	scheduler.recordTerminalSuccess(&published)

	snap := scheduler.readiness.Snapshot()
	if !snap.collectionSuccess || len(snap.candidateIDs) != 0 {
		t.Fatalf("collision-only snap = %+v", snap)
	}
}

func TestRDY006InsertedAndDuplicateAreHandoffCandidates(t *testing.T) {
	t.Parallel()

	scheduler := &leaseScheduler{readiness: &readinessTracker{}}
	published := sourceobservation.PublishBatchResult{Results: []sourceobservation.PublishedObservation{
		sourceobservation.NewPublishedObservation(11, sourceobservation.PublishInserted, 0),
		sourceobservation.NewPublishedObservation(12, sourceobservation.PublishDuplicate, 1),
		sourceobservation.NewPublishedObservation(13, sourceobservation.PublishCollision, 2),
	}}
	scheduler.recordTerminalSuccess(&published)

	snap := scheduler.readiness.Snapshot()
	if !snap.collectionSuccess || len(snap.candidateIDs) != 2 || snap.candidateIDs[0] != 11 || snap.candidateIDs[1] != 12 {
		t.Fatalf("candidate snap = %+v", snap)
	}
}

func TestRDY007DeadLetterFirstCandidateRecoversOnLaterAdd(t *testing.T) {
	t.Parallel()

	tracker := &readinessTracker{}
	tracker.ObserveCollectionSuccess()
	tracker.AddHandoffCandidates(11, 12)

	snap := tracker.Snapshot()
	if _, err := tracker.ApplyHandoff(snap, []handoffStatus{
		{ObservationID: 11, Status: new(contract.StatusDeadLetter)},
		{ObservationID: 12, Status: new(contract.StatusPending)},
	}); err != nil {
		t.Fatal(err)
	}

	snap = tracker.Snapshot()
	if snap.handoffCompleted || len(snap.candidateIDs) != 1 || snap.candidateIDs[0] != 12 {
		t.Fatalf("after DLQ snap = %+v", snap)
	}

	tracker.AddHandoffCandidates(13)

	snap = tracker.Snapshot()
	if state, err := tracker.ApplyHandoff(snap, []handoffStatus{
		{ObservationID: 12, Status: new(contract.StatusPending)},
		{ObservationID: 13, Status: new(contract.StatusProcessed)},
	}); err != nil || state != HandoffProcessed {
		t.Fatalf("recover state = %s, %v", state, err)
	}
}

func TestRDY008MissingFirstCandidateRecoversOnLaterAdd(t *testing.T) {
	t.Parallel()

	tracker := &readinessTracker{}
	tracker.ObserveCollectionSuccess()
	tracker.AddHandoffCandidates(21)

	if _, err := tracker.ApplyHandoff(tracker.Snapshot(), []handoffStatus{{ObservationID: 21}}); err != nil {
		t.Fatal(err)
	}

	if snap := tracker.Snapshot(); snap.handoffCompleted || len(snap.candidateIDs) != 0 {
		t.Fatalf("missing candidate snap = %+v", snap)
	}

	tracker.AddHandoffCandidates(22)

	if state, err := tracker.ApplyHandoff(tracker.Snapshot(), []handoffStatus{
		{ObservationID: 22, Status: new(contract.StatusProcessed)},
	}); err != nil || state != HandoffProcessed {
		t.Fatalf("recover state = %s, %v", state, err)
	}
}

func TestRDY009AnyProcessedCandidateLatchesReady(t *testing.T) {
	t.Parallel()

	deps := readyDeps()

	deps.scheduler = &stubSchedulerView{snap: SchedulerSnapshot{
		State: SchedulerRunning, QueueCapacity: 16, Discovered: 6, QueueFull: true, DiscoveryTruncated: true,
	}}
	deps.tracker.ObserveCollectionSuccess()
	deps.tracker.AddHandoffCandidates(31, 32)

	store := mustStubStore(t, deps.store)

	store.statuses = []handoffStatus{
		{ObservationID: 31, Status: new(contract.StatusPending)},
		{ObservationID: 32, Status: new(contract.StatusProcessed)},
	}

	body := evaluateReadiness(t.Context(), &deps)

	if body.State != ReadyReady || body.Status != "ready" || body.Dependency != "" || !body.HandoffProcessed {
		t.Fatalf("processed body = %+v", body)
	}

	if !body.QueueFull || !body.DiscoveryTruncated || body.DueJobs != 6 || body.DueJobsExact {
		t.Fatalf("queue/due fields = %+v", body)
	}

	if readinessHTTPStatus(&body) != http.StatusOK {
		t.Fatalf("http = %d", readinessHTTPStatus(&body))
	}
}

func TestRDY010UnknownOrMismatchedHandoffShapeIsDegraded(t *testing.T) {
	t.Parallel()

	tracker := &readinessTracker{}
	tracker.AddHandoffCandidates(41)

	weird := contract.Status("WEIRD")
	snap := tracker.Snapshot()

	if _, err := tracker.ApplyHandoff(snap, []handoffStatus{{ObservationID: 41, Status: &weird}}); err == nil {
		t.Fatal("unknown status must fail closed")
	}

	if _, err := tracker.ApplyHandoff(snap, nil); err == nil {
		t.Fatal("row count mismatch must fail closed")
	}

	ids, err := scanHandoffStatuses(&stubRows{rows: []stubRow{{id: 41, status: new("PENDING")}}}, []int64{41, 42})
	if err == nil || ids != nil {
		t.Fatalf("missing row shape = %v, %v", ids, err)
	}

	deps := readyDeps()
	deps.tracker.ObserveCollectionSuccess()
	deps.tracker.AddHandoffCandidates(41)

	store := mustStubStore(t, deps.store)

	store.statusErr = errors.New("shape")

	body := evaluateReadiness(t.Context(), &deps)

	if body.State != ReadyDegraded || body.Dependency != "postgres_queue" || body.PendingQueue != nil || body.Status != statusNotReady {
		t.Fatalf("shape failure body = %+v", body)
	}
}

func TestHandoffApplyPreservesCandidatesAddedAfterSnapshot(t *testing.T) {
	t.Parallel()

	tracker := &readinessTracker{}
	tracker.AddHandoffCandidates(51)

	snap := tracker.Snapshot()
	tracker.AddHandoffCandidates(52)

	state, err := tracker.ApplyHandoff(snap, []handoffStatus{{ObservationID: 51}})
	if err != nil {
		t.Fatal(err)
	}

	if state != HandoffNone {
		t.Fatalf("handoff state = %s, want %s", state, HandoffNone)
	}

	after := tracker.Snapshot()
	if after.handoffCompleted || len(after.candidateIDs) != 1 || after.candidateIDs[0] != 52 {
		t.Fatalf("snapshot-relative apply = %+v", after)
	}
}

func TestRDY011CandidateBurstIsBoundedAndMayFalseNegative(t *testing.T) {
	t.Parallel()

	tracker := &readinessTracker{}
	ids := make([]int64, 0, 40)

	for i := int64(1); i <= 40; i++ {
		ids = append(ids, i)
	}

	tracker.AddHandoffCandidates(ids...)

	snap := tracker.Snapshot()
	if len(snap.candidateIDs) != maxHandoffCandidates || snap.candidateIDs[0] != 9 || snap.candidateIDs[31] != 40 {
		t.Fatalf("bounded candidates = %v", snap.candidateIDs)
	}
}

func TestRDY012CompletedLatchSkipsHandoffQueryAndKeepsPendingProbe(t *testing.T) {
	t.Parallel()

	deps := readyDeps()
	deps.tracker.ObserveCollectionSuccess()
	deps.tracker.AddHandoffCandidates(51)

	if _, err := deps.tracker.ApplyHandoff(deps.tracker.Snapshot(), []handoffStatus{
		{ObservationID: 51, Status: new(contract.StatusProcessed)},
	}); err != nil {
		t.Fatal(err)
	}

	store := mustStubStore(t, deps.store)

	store.statusErr = errors.New("handoff must not be queried after latch")

	body := evaluateReadiness(t.Context(), &deps)

	if store.handoffCalls != 0 || store.pendingCalls != 1 {
		t.Fatalf("calls handoff=%d pending=%d", store.handoffCalls, store.pendingCalls)
	}

	if body.State != ReadyReady || body.PendingQueue == nil || *body.PendingQueue != 3 {
		t.Fatalf("latched body = %+v", body)
	}
}

func TestRDY013ReadyJSONKeepsLegacyFieldsAndAdditiveTypes(t *testing.T) {
	t.Parallel()

	deps := readyDeps()
	deps.tracker.ObserveCollectionSuccess()
	deps.tracker.AddHandoffCandidates(61)

	store := mustStubStore(t, deps.store)

	store.statuses = []handoffStatus{{ObservationID: 61, Status: new(contract.StatusProcessed)}}

	body := evaluateReadiness(t.Context(), &deps)

	raw, err := jsonv2.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any

	if err := jsonv2.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}

	for _, field := range []string{"status", "helper", "first_success", "handoff_status", "pending_queue"} {
		if _, ok := decoded[field]; !ok {
			t.Fatalf("missing legacy field %s in %s", field, raw)
		}
	}

	if decoded["status"] != "ready" || decoded["helper"] != "ok" || decoded["first_success"] != true {
		t.Fatalf("legacy values = %s", raw)
	}

	if decoded["handoff_status"] != "PROCESSED" || decoded["handoff_processed"] != true {
		t.Fatalf("handoff fields = %s", raw)
	}

	if _, ok := decoded["pending_queue"].(float64); !ok {
		t.Fatalf("pending_queue type = %T", decoded["pending_queue"])
	}

	if decoded["due_jobs_exact"] != false {
		t.Fatalf("due_jobs_exact = %v", decoded["due_jobs_exact"])
	}

	if _, ok := decoded["dependency"]; ok {
		t.Fatalf("READY must omit dependency: %s", raw)
	}
}

func TestRDY014StaleFreshnessDoesNotFailReadiness(t *testing.T) {
	t.Parallel()

	metrics := NewMetrics(prometheus.NewPedanticRegistry())
	started := time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC)
	metrics.ObserveSuccess(contract.ProviderYouTubeJS, "community_collect", started)
	metrics.ObserveFreshness(contract.ProviderYouTubeJS, "community_collect", started.Add(time.Hour))

	deps := readyDeps()
	deps.tracker.ObserveCollectionSuccess()
	deps.tracker.AddHandoffCandidates(71)

	store := mustStubStore(t, deps.store)

	store.statuses = []handoffStatus{{ObservationID: 71, Status: new(contract.StatusProcessed)}}

	body := evaluateReadiness(t.Context(), &deps)

	if body.State != ReadyReady || readinessHTTPStatus(&body) != http.StatusOK {
		t.Fatalf("stale freshness must not 503: %+v", body)
	}
}

func TestRDY016PR366SameRemoteReadinessBlock(t *testing.T) {
	t.Parallel()

	script, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "scripts", "deploy", "ap-deploy.sh"))
	if err != nil {
		t.Fatal(err)
	}

	text := string(script)
	source := strings.Index(text, ". scripts/deploy/lib/ap-collector-readiness.sh")
	validate := strings.Index(text, "collector_readiness_validate")

	if source < 0 || validate < 0 || source >= validate {
		t.Fatal("readiness helper source must precede collector_readiness_validate")
	}

	cwd := strings.LastIndex(text[:source], "cd ~/hololive-bot")
	if cwd < 0 {
		t.Fatal("remote repository cwd must precede readiness helper source")
	}
}

func TestReadinessHTTPStatusTable(t *testing.T) {
	t.Parallel()

	for state, want := range readinessHTTPStatusTable {
		body := readinessResponse{State: state, Status: want.ready}
		if want.dependency {
			body.Dependency = "scheduler"
		}

		if got := readinessHTTPStatus(&body); got != want.status {
			t.Fatalf("state %s http = %d, want %d", state, got, want.status)
		}
	}
}

func collectorInstanceIDForTest(appConfig *settings.YouTubeCollectorRuntimeConfig) string {
	if id := collectorInstanceID(appConfig); id != "" {
		return id
	}

	return runtimeName
}

func TestCollectorInstanceIDRuntimeNameFallbackIsTestOnly(t *testing.T) {
	t.Parallel()

	if got := collectorInstanceID(nil); got != "" {
		t.Fatalf("production instance ID fallback = %q", got)
	}

	if got := collectorInstanceIDForTest(nil); got != runtimeName {
		t.Fatalf("test fallback = %q", got)
	}
}

type stubHelper struct {
	exited bool
	proto  int
	health func(context.Context) error
}

func (s *stubHelper) Exited() bool { return s.exited }

func (s *stubHelper) Healthy(ctx context.Context) error {
	if s.health != nil {
		if err := s.health(ctx); err != nil {
			return fmt.Errorf("health: %w", err)
		}

		return nil
	}

	return nil
}

func (s *stubHelper) ProtocolVersion() int {
	if s.proto == 0 {
		return 1
	}

	return s.proto
}

type stubSchedulerView struct {
	snap SchedulerSnapshot
}

func (s *stubSchedulerView) Snapshot() SchedulerSnapshot { return s.snap }

type stubStore struct {
	pending      BoundedCount
	pendingErr   error
	statuses     []handoffStatus
	statusErr    error
	handoffCalls int
	pendingCalls int
}

func (s *stubStore) CountPending(ctx context.Context, _ int) (BoundedCount, error) {
	s.pendingCalls++

	if err := ctx.Err(); err != nil {
		return BoundedCount{}, fmt.Errorf("count pending: %w", err)
	}

	return s.pending, s.pendingErr
}

func (s *stubStore) LoadHandoffStatuses(ctx context.Context, ids []int64) ([]handoffStatus, error) {
	s.handoffCalls++

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("load handoff statuses: %w", err)
	}

	if s.statusErr != nil {
		return nil, s.statusErr
	}

	if s.statuses != nil {
		return s.statuses, nil
	}

	out := make([]handoffStatus, len(ids))
	for i, id := range ids {
		out[i] = handoffStatus{ObservationID: id, Status: new(contract.StatusPending)}
	}

	return out, nil
}

type stubRow struct {
	id     int64
	status *string
}

type stubRows struct {
	rows []stubRow
	i    int
}

func (s *stubRows) Next() bool {
	if s.i >= len(s.rows) {
		return false
	}

	s.i++

	return true
}

func (s *stubRows) Scan(dest ...any) error {
	if len(dest) != 2 {
		return fmt.Errorf("scan handoff status: destination count = %d", len(dest))
	}

	id, ok := dest[0].(*int64)
	if !ok {
		return errors.New("scan handoff status: observation ID destination is invalid")
	}

	status, ok := dest[1].(**string)
	if !ok {
		return errors.New("scan handoff status: status destination is invalid")
	}

	row := s.rows[s.i-1]

	*id = row.id
	*status = row.status

	return nil
}

func (s *stubRows) Err() error { return nil }

func readyDeps() readinessDeps {
	pending := 3

	return readinessDeps{
		instanceID:    "youtube-collector-c",
		helperTimeout: time.Second,
		dbTimeout:     time.Second,
		pendingCap:    pendingQueueCap,
		scheduler:     &stubSchedulerView{snap: SchedulerSnapshot{State: SchedulerRunning, QueueCapacity: 16, Discovered: 6}},
		helper:        &stubHelper{},
		store:         &stubStore{pending: BoundedCount{Value: pending}},
		tracker:       &readinessTracker{},
	}
}

func mustStubStore(t *testing.T, store queueStore) *stubStore {
	t.Helper()

	typed, ok := store.(*stubStore)
	if !ok {
		t.Fatalf("queue store type = %T, want *stubStore", store)
	}

	return typed
}
