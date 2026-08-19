package botruntime

import (
	"context"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/panicguard"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
	"github.com/park285/shared-go/pkg/workercontract"
)

func (r *BotRuntime) WorkerRegistrations() []workercontract.Registration {
	if r == nil || r.durable == nil {
		return nil
	}
	durable := r.durable
	return []workercontract.Registration{
		{
			WorkerID:          "bot_webhook_inbox",
			Runtime:           workercontract.RuntimeGo,
			QueueBackend:      workercontract.QueuePostgres,
			QueueScope:        workercontract.QueueScopeShared,
			SettingsValidated: true,
			ExecutorSnapshot:  func() workercontract.ExecutorSnapshot { return durable.inboxTracker.Snapshot(time.Now()) },
			QueueSnapshot:     durable.inboxSampler.Latest,
			Counters:          durable.inboxTotals,
		},
		{
			WorkerID:          "bot_reply_outbox",
			Runtime:           workercontract.RuntimeGo,
			QueueBackend:      workercontract.QueuePostgres,
			QueueScope:        workercontract.QueueScopeShared,
			SettingsValidated: true,
			ExecutorSnapshot:  func() workercontract.ExecutorSnapshot { return durable.outboxTracker.Snapshot(time.Now()) },
			QueueSnapshot:     durable.outboxSampler.Latest,
			Counters:          durable.outboxTotals,
		},
	}
}

func (r *BotRuntime) InstallWorkerRegistry(ctx context.Context, registry *workercontract.Registry, checker *workercontract.ProfileFileChecker) {
	if r == nil {
		return
	}
	r.workerRegistry = registry
	r.workerProfileChecker = checker
	if r.Config != nil && strings.TrimSpace(r.Config.Server.MetricsAddr) != "" {
		r.MetricsServer = sharedserver.NewMetricsServer(ctx, r.Config.Server.MetricsAddr, r.Config.Server.APIKey, registry)
	}
}

func (r *BotRuntime) startWorkerProfileChecker(ctx context.Context) {
	if r == nil || r.workerProfileChecker == nil {
		return
	}
	panicguard.Go(r.Logger, "stack-worker-profile-checker", func() { r.workerProfileChecker.Run(ctx) })
}
