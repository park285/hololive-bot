package botruntime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/quic-go/quic-go/http3"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/broadcasttypedecision"
	"github.com/kapu/hololive-api/internal/service/acl"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

type BotRuntime struct {
	Config *apiconfig.BotPlaneConfig
	Logger *slog.Logger

	Bot *orchestration.Bot

	IrisRooms appbootstrap.IrisRoomLister

	ServerAddr      string
	H3Server        *http3.Server
	ShortLinkServer *http.Server
	MetricsServer   *http.Server
	PprofServer     *http.Server

	h3CertReloadStart func(context.Context)

	webhookHandlerCloser interface{ CloseContext(context.Context) error }
	durable              *durableRuntime
	broadcastTypes       *broadcasttypedecision.Worker
	workerRegistry       *workercontract.Registry
	workerProfileChecker *workercontract.ProfileFileChecker

	lifecycleMu        sync.Mutex
	started            bool
	stopping           bool
	tasksCancel        context.CancelFunc
	tasksWG            sync.WaitGroup
	tasksDone          chan struct{}
	backgroundErr      error
	shutdownErr        error
	quiesced           atomic.Bool
	cleanup            func() error
	resourcesCloseOnce sync.Once
	resourcesDone      chan struct{}
	resourcesErr       error
	requestsMu         sync.Mutex
	requestsWG         sync.WaitGroup
	requestsDone       chan struct{}
	requestsClosing    bool
	httpHandlersOnce   sync.Once
}

type Dependencies struct {
	ACL     *acl.Service
	Holodex *holodexprovider.Service
}

func BuildRuntime(ctx context.Context, appConfig *apiconfig.BotPlaneConfig, shared *sharedmodules.InfraModule, deps Dependencies, logger *slog.Logger) (*BotRuntime, error) {
	ctx, logger, err := bootstrap.NormalizeRuntimeBuildInputs(ctx, appConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("normalize runtime build inputs: %w", err)
	}

	infra, err := appbootstrap.InitBotInfrastructure(ctx, appConfig, shared, appbootstrap.PlaneDependencies{ACL: deps.ACL, Holodex: deps.Holodex}, logger)
	if err != nil {
		return nil, fmt.Errorf("init bot infrastructure: %w", err)
	}

	var broadcastTypes *broadcasttypedecision.Worker

	if keyFile := appConfig.BroadcastTypeGeminiKeyFile; keyFile != "" {
		broadcastTypes, err = newBroadcastTypeDecisionWorker(keyFile, infra, logger)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("build broadcast type decision worker: %w", err), infra.Cleanup())
		}
	}

	runtime, err := buildBotRuntime(ctx, appConfig, logger, infra)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("build bot runtime: %w", err), infra.Cleanup())
	}

	runtime.broadcastTypes = broadcastTypes
	runtime.cleanup = infra.Cleanup
	runtime.IrisRooms = infra.IrisRoomLister

	return runtime, nil
}

func (r *BotRuntime) Close() {
	if r == nil {
		return
	}

	if err := r.CloseContext(context.Background()); err != nil {
		r.logError("bot runtime close failed", err)
	}
}
