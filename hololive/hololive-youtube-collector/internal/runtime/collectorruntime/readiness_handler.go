package collectorruntime

import (
	"context"
	jsonv2 "encoding/json/v2"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/ginjson"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

type collectorReadiness struct {
	appConfig *settings.YouTubeCollectorRuntimeConfig
	infra     *collectorInfrastructure
	scheduler *leaseScheduler
	tracker   *readinessTracker
	disabled  bool
}

type readinessResponse struct {
	Status                string         `json:"status"`
	Runtime               string         `json:"runtime"`
	InstanceID            string         `json:"instance_id"`
	State                 ReadinessState `json:"state"`
	Dependency            string         `json:"dependency,omitempty"`
	Helper                string         `json:"helper"`
	HelperProtocolVersion int            `json:"helper_protocol_version"`
	FirstSuccess          bool           `json:"first_success"`
	HandoffStatus         HandoffState   `json:"handoff_status"`
	HandoffProcessed      bool           `json:"handoff_processed"`
	HandoffCandidates     int            `json:"handoff_candidates"`
	PendingQueue          *int           `json:"pending_queue"`
	PendingQueueCapped    bool           `json:"pending_queue_capped"`
	DueJobs               int            `json:"due_jobs"`
	DueJobsExact          bool           `json:"due_jobs_exact"`
	QueueDepth            int            `json:"queue_depth"`
	QueueCapacity         int            `json:"queue_capacity"`
	QueueFull             bool           `json:"queue_full"`
	DiscoveryTruncated    bool           `json:"discovery_truncated"`
}

func (r *collectorReadiness) configure(opts *sharedserver.RuntimeRouterOptions) {
	r.tracker = r.scheduler.readinessTrackerRef()
	opts.EnableGzip = true
	opts.ReadyResponder = r.respond
}

func (r *collectorReadiness) respond(c *gin.Context) {
	if r.disabled {
		r.respondDisabled(c)

		return
	}

	cfg := settings.YouTubeCollectorConfig{}

	if r.appConfig != nil {
		cfg = r.appConfig.Collector
	}

	probeCtx, cancel := context.WithTimeout(c.Request.Context(), cfg.ReadinessTimeout)

	defer cancel()

	deps := r.deps(&cfg)
	body := evaluateReadiness(probeCtx, &deps)

	payload, err := jsonv2.Marshal(body)
	if err != nil {
		fallback := readinessResponse{Runtime: runtimeName, Helper: helperNotReady}

		fallback = notReady(&fallback, ReadyDegraded, "scheduler")
		c.Status(readinessHTTPStatus(&fallback))

		return
	}

	c.Data(readinessHTTPStatus(&body), gin.MIMEJSON, payload)
}

func (r *collectorReadiness) respondDisabled(c *gin.Context) {
	capacity := 0

	if r.appConfig != nil && r.appConfig.WorkerProfile != nil {
		worker := r.appConfig.WorkerProfile.Loaded.Profile.Workers["collection"]
		if worker.Queue.Capacity.Items != nil {
			capacity = int(*worker.Queue.Capacity.Items)
		}
	}

	ginjson.Respond(c, 200, readinessResponse{
		Status: "ready", Runtime: runtimeName, InstanceID: collectorInstanceID(r.appConfig), State: ReadyReady,
		Helper: "disabled", HandoffStatus: HandoffNone, DueJobsExact: true, QueueCapacity: capacity,
	})
}

func (r *collectorReadiness) deps(cfg *settings.YouTubeCollectorConfig) readinessDeps {
	var helper helperHealth

	if r != nil && r.infra != nil && r.infra.youtubejs != nil {
		helper = r.infra.youtubejs
	}

	var sched schedulerView

	if r != nil && r.scheduler != nil {
		sched = r.scheduler
	}

	return readinessDeps{
		instanceID:    collectorInstanceID(r.appConfig),
		helperTimeout: cfg.HelperHealthTimeout,
		dbTimeout:     cfg.DBTimeout,
		pendingCap:    pendingQueueCap,
		scheduler:     sched,
		helper:        helper,
		store:         queueStoreFrom(r.infra),
		tracker:       r.tracker,
	}
}

func collectorInstanceID(appConfig *settings.YouTubeCollectorRuntimeConfig) string {
	if appConfig == nil {
		return ""
	}

	return strings.TrimSpace(appConfig.Collector.InstanceID)
}
