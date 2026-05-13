package poller

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

type PendingPublishedAtResolver struct {
	db                *gorm.DB
	client            *scraper.Client
	routeDecider      NotificationRouteDecider
	interval          time.Duration
	batchSize         int
	maxResolvePerRun  int
	maxRunDuration    time.Duration
	resolveTimeout    time.Duration
	minDetectedAge    time.Duration
	failureBackoffTTL time.Duration
	logger            *slog.Logger
}

var errResolverParentCanceled = errors.New("pending published_at resolver parent context canceled")

type publishedAtResolverCandidateResult struct {
	processed int
	stop      bool
}

func NewPendingPublishedAtResolver(
	db *gorm.DB,
	client *scraper.Client,
	routeDecider NotificationRouteDecider,
	interval time.Duration,
	batchSize int,
	logger *slog.Logger,
) *PendingPublishedAtResolver {
	return NewPendingPublishedAtResolverWithControls(
		db,
		client,
		routeDecider,
		interval,
		batchSize,
		batchSize,
		12*time.Second,
		10*time.Second,
		20*time.Second,
		5*time.Minute,
		logger,
	)
}

func NewPendingPublishedAtResolverWithControls(
	db *gorm.DB,
	client *scraper.Client,
	routeDecider NotificationRouteDecider,
	interval time.Duration,
	batchSize int,
	maxResolvePerRun int,
	maxRunDuration time.Duration,
	resolveTimeout time.Duration,
	minDetectedAge time.Duration,
	failureBackoffTTL time.Duration,
	logger *slog.Logger,
) *PendingPublishedAtResolver {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 50
	}
	if maxResolvePerRun <= 0 {
		maxResolvePerRun = batchSize
	}
	if maxRunDuration <= 0 {
		maxRunDuration = 12 * time.Second
	}
	if resolveTimeout <= 0 {
		resolveTimeout = 10 * time.Second
	}
	if minDetectedAge <= 0 {
		minDetectedAge = 20 * time.Second
	}
	if failureBackoffTTL <= 0 {
		failureBackoffTTL = 5 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &PendingPublishedAtResolver{
		db:                db,
		client:            client,
		routeDecider:      routeDecider,
		interval:          interval,
		batchSize:         batchSize,
		maxResolvePerRun:  maxResolvePerRun,
		maxRunDuration:    maxRunDuration,
		resolveTimeout:    resolveTimeout,
		minDetectedAge:    minDetectedAge,
		failureBackoffTTL: failureBackoffTTL,
		logger:            logger,
	}
}

func (r *PendingPublishedAtResolver) Start(ctx context.Context) {
	if !r.canStart() {
		return
	}

	for {
		r.runResolverIteration(ctx)
		if !r.waitResolverInterval(ctx) {
			return
		}
	}
}

func (r *PendingPublishedAtResolver) canStart() bool {
	return r != nil && r.db != nil && r.client != nil
}

func (r *PendingPublishedAtResolver) runResolverIteration(ctx context.Context) {
	if err := r.RunOnce(ctx); err != nil && ctx.Err() == nil {
		r.logger.Warn("Pending published_at resolver iteration failed",
			slog.Any("error", err),
		)
	}
}

func (r *PendingPublishedAtResolver) waitResolverInterval(ctx context.Context) bool {
	timer := time.NewTimer(r.resolverInterval())
	select {
	case <-ctx.Done():
		stopAndDrainTimer(timer)
		return false
	case <-timer.C:
		return true
	}
}

func stopAndDrainTimer(timer *time.Timer) {
	if timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}
