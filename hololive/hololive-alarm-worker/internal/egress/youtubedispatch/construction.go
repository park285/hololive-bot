// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package youtubedispatch

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/template"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/telemetry"
)

const defaultLogicalGroupScanLimit = 100

// Dependencies는 dispatcher의 저장소와 메시지 전달 의존성을 명시한다.
type Dependencies struct {
	DB             deliverysql.DeliveryDB
	Cache          cache.Client
	Sender         delivery.MessageSender
	Renderer       *template.Renderer
	MessageStrings *messagestrings.Store
}

// NewDispatcher는 의존성을 연결하고 전이 저장소 초기화 오류를 반환한다. 작업 루프는 시작하지 않는다.
func NewDispatcher(deps Dependencies, logger *slog.Logger, config *dispatchstate.Config) (*Dispatcher, error) {
	if deps.DB != nil && deliverysql.IsNilDB(deps.DB) {
		return nil, errors.New("initialize youtube dispatcher: db contains a nil value")
	}

	initOutboxMetrics()

	logger = dispatcherLogger(logger)

	normalizedConfig := normalizedDispatcherConfig(config)

	var transitionStore *store.TransitionStore

	if deps.DB != nil {
		var err error

		transitionStore, err = newDispatcherTransitionStore(deps.DB, logger, normalizedConfig)
		if err != nil {
			return nil, fmt.Errorf("initialize youtube dispatcher: %w", err)
		}
	}

	telemetryRepository := newDispatcherTelemetryRepository(deps.DB)

	return assembleDispatcher(deps, logger, normalizedConfig, telemetryRepository, transitionStore), nil
}

func dispatcherLogger(logger *slog.Logger) *slog.Logger {
	if logger != nil {
		return logger
	}

	return slog.Default()
}

func normalizedDispatcherConfig(config *dispatchstate.Config) dispatchstate.Config {
	normalized := dispatchstate.NormalizeDispatcherConfig(config)
	defaults := dispatchstate.DefaultConfig()

	if normalized.MaxRetries <= 0 {
		normalized.MaxRetries = defaults.MaxRetries
	}

	if normalized.RetryBackoff <= 0 {
		normalized.RetryBackoff = defaults.RetryBackoff
	}

	return normalized
}

func newDispatcherTelemetryRepository(querier dbx.Querier) *telemetry.Repository {
	if querier == nil {
		return nil
	}

	return telemetry.NewRepository(querier)
}

func newDispatcherTransitionStore(
	db deliverysql.DeliveryDB,
	logger *slog.Logger,
	config dispatchstate.Config,
) (*store.TransitionStore, error) {
	transitionStore, err := store.NewTransitionStore(db, logger, store.TransitionConfig{
		MaxRetries: config.MaxRetries, RetryBackoff: config.RetryBackoff,
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
	config dispatchstate.Config,
	telemetryRepository *telemetry.Repository,
	transitionStore *store.TransitionStore,
) *Dispatcher {
	deliveryRepo := store.NewDeliveryRepository(deps.DB, logger)

	tp := newTelemetryProcessor(telemetryRepository, logger, &config)
	al := newAuditLogger(telemetryRepository, deliveryRepo, logger, &config, tp)
	grouper := newOutboxGrouper(deps.DB, deps.Cache, logger, &config)
	formatter := newMessageFormatter(deps.Renderer, deps.Cache, logger, deps.MessageStrings)

	claimManager := newClaimManager(deps.DB, logger, &config, deliveryRepo, transitionStore, nil, grouper, al)
	metricsRecorder := newMetricsRecorder(logger, al, claimManager)
	sendEngine := newSendEngine(
		deps.Sender, formatter, logger, &config, claimManager, al, metricsRecorder, dispatcherTransitions(transitionStore)...,
	)
	claimManager.setExecutor(sendEngine)
	claimManager.setMetricsRecorder(metricsRecorder)

	return &Dispatcher{
		claim:     claimManager,
		send:      sendEngine,
		telemetry: tp,
		audit:     al,
		metrics:   metricsRecorder,
		grouper:   grouper,
		logger:    logger,
		config:    config,
	}
}

func dispatcherTransitions(transitionStore *store.TransitionStore) []deliveryTransition {
	if transitionStore == nil {
		return nil
	}

	return []deliveryTransition{transitionStore}
}
