package youtubedispatch

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/reflectutil"

	"github.com/kapu/hololive-alarm-worker/internal/egress/wakeupwait"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/format"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/deliverysql"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/telemetry"
	"github.com/kapu/hololive-alarm-worker/internal/leaseowner"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/wakeup"
)

const defaultLogicalGroupScanLimit = 100

type Dependencies struct {
	DB             deliverysql.DeliveryDB
	Cache          alarmstore.SubscriberCache
	Sender         delivery.PreparedMessageSender
	Renderer       *template.Renderer
	MessageStrings *messagestrings.Store
	MemberNames    format.MemberNameSource
	WakeupCache    cache.LowLevelCache
}

func NewDispatcher(deps Dependencies, logger *slog.Logger, config *Config) (*Dispatcher, error) {
	if deps.DB == nil || reflectutil.IsNil(deps.DB) {
		return nil, errors.New("initialize youtube dispatcher: db is required")
	}

	if deps.MemberNames == nil {
		return nil, errors.New("initialize youtube dispatcher: member name source is required")
	}

	if config == nil {
		return nil, errors.New("initialize youtube dispatcher: config is required")
	}

	validConfig := *config
	if err := validConfig.Validate(); err != nil {
		return nil, fmt.Errorf("initialize youtube dispatcher: %w", err)
	}

	outboxMetrics()

	logger = dispatcherLogger(logger)

	transitionStore, err := newDispatcherTransitionStore(deps.DB, logger, validConfig)
	if err != nil {
		return nil, fmt.Errorf("initialize youtube dispatcher: %w", err)
	}

	telemetryRepository, err := telemetry.NewRepository(deps.DB)
	if err != nil {
		return nil, fmt.Errorf("initialize youtube dispatcher: telemetry repository: %w", err)
	}

	idle, err := wakeupwait.NewDelivery(deps.WakeupCache, wakeup.YouTubeDelivery, validConfig.PollInterval, logger)
	if err != nil {
		return nil, fmt.Errorf("initialize youtube dispatcher: %w", err)
	}

	dispatcher := assembleDispatcher(deps, logger, validConfig, telemetryRepository, transitionStore)

	dispatcher.idle = idle

	return dispatcher, nil
}

func dispatcherLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}

	return slog.Default()
}

func newDispatcherTransitionStore(
	db deliverysql.DeliveryDB,
	logger *slog.Logger,
	config Config,
) (*store.TransitionStore, error) {
	owner, err := leaseowner.InstanceID("youtube-dispatcher")
	if err != nil {
		return nil, fmt.Errorf("initialize youtube delivery transition store: owner: %w", err)
	}

	transitionStore, err := store.NewTransitionStore(db, logger, store.TransitionConfig{
		Owner: owner, MaxRetries: config.MaxRetries, RetryBackoff: config.RetryBackoff,
		LockTimeout: config.LockTimeout, ClaimFreshnessWindow: config.ClaimFreshnessWindow,
		LogicalGroupLimit: defaultLogicalGroupScanLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize youtube delivery transition store: %w", err)
	}

	return transitionStore, nil
}

func assembleDispatcher(
	deps Dependencies,
	logger *slog.Logger,
	config Config,
	telemetryRepository *telemetry.Repository,
	transitionStore *store.TransitionStore,
) *Dispatcher {
	deliveryRepo := store.NewDeliveryRepository(deps.DB, logger)

	tp := newTelemetryProcessor(telemetryRepository, logger, &config)
	al := newAuditLogger(telemetryRepository, deliveryRepo, logger, &config)
	grouper := newOutboxGrouper(deps.DB, deps.Cache, logger, &config)
	formatter := format.NewMessageFormatter(deps.Renderer, deps.MemberNames, deps.MessageStrings)

	metricsRecorder := newMetricsRecorder(logger, al)
	sendEngine := newSendEngine(deps.Sender, formatter, logger, &config, al, metricsRecorder, transitionStore)
	claimManager := newClaimManager(deps.DB, logger, &config, deliveryRepo, transitionStore, sendEngine, grouper, al)

	return &Dispatcher{
		claim:           claimManager,
		send:            sendEngine,
		telemetry:       tp,
		audit:           al,
		metrics:         metricsRecorder,
		grouper:         grouper,
		logger:          logger,
		config:          config,
		cleanupInterval: outboxCleanupLoopInterval,
	}
}
