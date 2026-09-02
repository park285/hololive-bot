package collectorruntime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

type (
	ReadinessState string
	HandoffState   string
)

const (
	ReadyStarting          ReadinessState = "STARTING"
	ReadyWaitingCollection ReadinessState = "WAITING_COLLECTION"
	ReadyWaitingHandoff    ReadinessState = "WAITING_HANDOFF"
	ReadyReady             ReadinessState = "READY"
	ReadyDegraded          ReadinessState = "DEGRADED"

	HandoffNone       HandoffState = "NONE"
	HandoffPending    HandoffState = "PENDING"
	HandoffProcessing HandoffState = "PROCESSING"
	HandoffProcessed  HandoffState = "PROCESSED"

	maxHandoffCandidates = 32
	pendingQueueCap      = 10_000
	helperReady          = "ok"
	helperNotReady       = "error"
	statusNotReady       = "not_ready"
)

type helperHealth interface {
	Exited() bool
	Healthy(context.Context) error
	ProtocolVersion() int
}

type queueStore interface {
	LoadHandoffStatuses(context.Context, []int64) ([]handoffStatus, error)
	CountPending(context.Context, int) (BoundedCount, error)
}

type schedulerView interface {
	Snapshot() SchedulerSnapshot
}

type readinessDeps struct {
	instanceID    string
	helperTimeout time.Duration
	dbTimeout     time.Duration
	pendingCap    int
	scheduler     schedulerView
	helper        helperHealth
	store         queueStore
	tracker       *readinessTracker
}

func evaluateReadiness(ctx context.Context, deps *readinessDeps) readinessResponse {
	body := newReadinessResponse(deps)
	if dependency := schedulerDependency(deps, &body); dependency != "" {
		return notReady(&body, ReadyDegraded, dependency)
	}

	if dependency := helperDependency(ctx, deps, &body); dependency != "" {
		return notReady(&body, ReadyDegraded, dependency)
	}

	if dependency := queueDependency(ctx, deps, &body); dependency != "" {
		return notReady(&body, ReadyDegraded, dependency)
	}

	return collectionReadiness(ctx, deps, &body)
}

func newReadinessResponse(deps *readinessDeps) readinessResponse {
	snap := SchedulerSnapshot{}

	if deps.scheduler != nil {
		snap = deps.scheduler.Snapshot()
	}

	return readinessResponse{
		Runtime:            runtimeName,
		InstanceID:         deps.instanceID,
		Helper:             helperNotReady,
		HandoffStatus:      HandoffNone,
		DueJobs:            snap.Discovered,
		DueJobsExact:       false,
		QueueDepth:         snap.QueueDepth,
		QueueCapacity:      snap.QueueCapacity,
		QueueFull:          snap.QueueFull,
		DiscoveryTruncated: snap.DiscoveryTruncated,
	}
}

func schedulerDependency(deps *readinessDeps, body *readinessResponse) string {
	if deps.scheduler == nil {
		return "scheduler"
	}

	snap := deps.scheduler.Snapshot()

	body.DueJobs = snap.Discovered
	body.QueueDepth = snap.QueueDepth
	body.QueueCapacity = snap.QueueCapacity
	body.QueueFull = snap.QueueFull
	body.DiscoveryTruncated = snap.DiscoveryTruncated

	if snap.State != SchedulerRunning {
		return "scheduler"
	}

	return ""
}

func helperDependency(ctx context.Context, deps *readinessDeps, body *readinessResponse) string {
	if deps.helper == nil || deps.helper.Exited() {
		return "youtubejs"
	}

	body.HelperProtocolVersion = deps.helper.ProtocolVersion()

	healthCtx, cancel := withRemainingTimeout(ctx, deps.helperTimeout)

	defer cancel()

	if err := deps.helper.Healthy(healthCtx); err != nil {
		return "youtubejs"
	}

	body.Helper = helperReady
	body.HelperProtocolVersion = deps.helper.ProtocolVersion()

	return ""
}

func queueDependency(ctx context.Context, deps *readinessDeps, body *readinessResponse) string {
	if deps.store == nil {
		return "postgres_queue"
	}

	dbCtx, cancel := withRemainingTimeout(ctx, deps.dbTimeout)

	defer cancel()

	pending, err := deps.store.CountPending(dbCtx, deps.pendingCap)
	if err != nil {
		return "postgres_queue"
	}

	value := pending.Value

	body.PendingQueue = &value
	body.PendingQueueCapped = pending.Capped

	return ""
}

func collectionReadiness(ctx context.Context, deps *readinessDeps, body *readinessResponse) readinessResponse {
	if deps.tracker == nil {
		return notReady(body, ReadyDegraded, "first_success")
	}

	snap := deps.tracker.Snapshot()

	body.FirstSuccess = snap.collectionSuccess
	body.HandoffCandidates = len(snap.candidateIDs)

	if !snap.collectionSuccess {
		return notReady(body, ReadyWaitingCollection, "first_success")
	}

	state, err := probeHandoff(ctx, deps, snap)
	if err != nil {
		body.PendingQueue = nil
		body.PendingQueueCapped = false

		return notReady(body, ReadyDegraded, "postgres_queue")
	}

	after := deps.tracker.Snapshot()

	body.HandoffStatus = state
	body.HandoffCandidates = len(after.candidateIDs)
	body.HandoffProcessed = state == HandoffProcessed

	if state != HandoffProcessed {
		return notReady(body, ReadyWaitingHandoff, "observation_handoff")
	}

	body.Status = "ready"
	body.State = ReadyReady

	return *body
}

func probeHandoff(ctx context.Context, deps *readinessDeps, snap readinessTrackerSnapshot) (HandoffState, error) {
	if snap.handoffCompleted {
		return HandoffProcessed, nil
	}

	if len(snap.candidateIDs) == 0 {
		return HandoffNone, nil
	}

	if deps.store == nil {
		return HandoffNone, errors.New("load observation handoff status: postgres pool is required")
	}

	dbCtx, cancel := withRemainingTimeout(ctx, deps.dbTimeout)

	defer cancel()

	statuses, err := deps.store.LoadHandoffStatuses(dbCtx, snap.candidateIDs)
	if err != nil {
		return HandoffNone, fmt.Errorf("load handoff statuses: %w", err)
	}

	out, err := deps.tracker.ApplyHandoff(snap, statuses)
	if err != nil {
		return out, fmt.Errorf("apply handoff: %w", err)
	}

	return out, nil
}

func notReady(body *readinessResponse, state ReadinessState, dependency string) readinessResponse {
	body.Status = statusNotReady
	body.State = state
	body.Dependency = dependency
	body.HandoffProcessed = body.HandoffStatus == HandoffProcessed

	return *body
}

func withRemainingTimeout(ctx context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}

	remaining := limit

	if deadline, ok := ctx.Deadline(); ok {
		until := time.Until(deadline)
		if until < remaining || remaining <= 0 {
			remaining = until
		}
	}

	if remaining <= 0 {
		return context.WithTimeout(ctx, time.Nanosecond)
	}

	return context.WithTimeout(ctx, remaining)
}

func newBoundedCount(value, limit int) (BoundedCount, error) {
	if value < 0 || value > limit+1 {
		return BoundedCount{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "pending queue count is outside bounds")
	}

	if value == limit+1 {
		return BoundedCount{Value: limit, Capped: true}, nil
	}

	return BoundedCount{Value: value, Capped: false}, nil
}

func readinessHTTPStatus(body *readinessResponse) int {
	if body.State == ReadyReady && body.Status == "ready" && body.Dependency == "" {
		return http.StatusOK
	}

	return http.StatusServiceUnavailable
}
