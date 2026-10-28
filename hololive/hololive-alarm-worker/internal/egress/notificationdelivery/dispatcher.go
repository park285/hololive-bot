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

// Package notificationdelivery는 alarm-worker가 소유하는 notification_delivery_outbox 발송기다. producer 적재는
// hololive-shared delivery.OutboxRepository에 남고, claim·요청 준비·발송 정산·유지보수만 이 패키지가 맡는다.
package notificationdelivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/park285/iris-client-go/v4/iris"
	"github.com/park285/shared-go/v2/pkg/db/pgxdb"
	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
	"github.com/park285/shared-go/v2/pkg/workercontract"

	workerconfig "github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendfence"
	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
	"github.com/kapu/hololive-alarm-worker/internal/leaseowner"
	deliverypayload "github.com/kapu/hololive-shared/pkg/contracts/notificationdelivery"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/panicscope"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type deliveryOutboxClaimer interface {
	fetchReadyAndLock(ctx context.Context, workerID string, batchSize int, lease time.Duration, activeRooms []string, processedIDs []int64) ([]domain.NotificationDeliveryOutbox, error)
}

// deliveryRequestStore와 deliveryOutboxTransitioner의 소유자 전이는 claim 당시 attemptCount를 함께 받고, fence가
// 맞지 않으면 행을 바꾸지 않은 채 *sendfence.MissError를 돌려준다.
type deliveryRequestStore interface {
	reissueFailedRequest(ctx context.Context, id int64, workerID string, attemptCount, generation int, exhaust bool, maxRetries int, backoff time.Duration, reason string) error
	saveRequest(ctx context.Context, id int64, workerID string, attemptCount int, request *deliverypayload.Request) error
	markRequestExhausted(ctx context.Context, id int64, workerID string, attemptCount int, errMsg string) error
}

type deliveryOutboxTransitioner interface {
	MarkSending(ctx context.Context, id int64, workerID string, attemptCount int, lease time.Duration) error
	MarkSent(ctx context.Context, id int64, workerID string, attemptCount int, irisRequestID string, handoffHorizon time.Duration) error
	MarkQuarantined(ctx context.Context, id int64, workerID string, attemptCount int, reason string) error
	MarkFailed(ctx context.Context, id int64, workerID string, attemptCount, maxRetries int, backoff time.Duration, errMsg string) error
}

type deliveryOutboxMaintainer interface {
	QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int64, error)
	CountFailed(ctx context.Context) (int64, error)
	Cleanup(ctx context.Context, olderThan time.Duration) (int64, error)
}

type deliveryRepository interface {
	deliveryRequestStore
	deliveryOutboxClaimer
	deliveryOutboxTransitioner
	deliveryOutboxMaintainer
}

const deliveryMaintenanceTimeout = 10 * time.Second

// DispatcherConfig의 값은 alarm-worker profile의 notification_delivery 항목이 정본이다.
// 프로필 없이 호출하는 생성자도 잘못된 값을 거부하며 lease 관계는 config의 공통 정책으로 검증한다.
type DispatcherConfig struct {
	AttemptTimeout            time.Duration
	BatchSize                 int
	MaxConcurrent             int
	MaxRetries                int
	PollInterval              time.Duration
	RetryBackoff              time.Duration
	CleanupAfter              time.Duration
	CleanupInterval           time.Duration
	CleanupEnabled            bool
	StaleSendingAfter         time.Duration
	StaleSendingSweepInterval time.Duration
	StaleSendingSweepLimit    int
}

type Dispatcher struct {
	repository    deliveryRepository
	sender        delivery.PreparedMessageSender
	logger        *slog.Logger
	config        DispatcherConfig
	workerID      string
	workerTracker *workercontract.ExecutorTracker
	workerTotals  *workercontract.Counters
}

func (d *Dispatcher) SetWorkerInstrumentation(tracker *workercontract.ExecutorTracker, totals *workercontract.Counters) {
	if d == nil {
		return
	}

	d.workerTracker = tracker
	d.workerTotals = totals
}

// NewDispatcher는 "delivery-dispatcher:hostname:pid"를 lease owner로 쓴다. 호스트 이름을 얻지 못하면 다른 이름으로
// 바꾸지 않고 생성 오류다.
func NewDispatcher(repository deliveryRepository, sender delivery.PreparedMessageSender, logger *slog.Logger, config *DispatcherConfig) (*Dispatcher, error) {
	if logger == nil {
		logger = slog.Default()
	}

	if config == nil {
		return nil, errors.New("new delivery dispatcher: config is required")
	}

	if sender == nil {
		return nil, errors.New("new delivery dispatcher: prepared sender is required")
	}

	cfg := *config
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("new delivery dispatcher: %w", err)
	}

	workerID, err := leaseowner.InstanceID("delivery-dispatcher")
	if err != nil {
		return nil, fmt.Errorf("new delivery dispatcher: %w", err)
	}

	return &Dispatcher{repository: repository, sender: sender, logger: logger, config: cfg, workerID: workerID}, nil
}

func (c *DispatcherConfig) validate() error {
	required := []struct {
		name  string
		valid bool
	}{
		{"batch size", c.BatchSize > 0},
		{"max concurrent", c.MaxConcurrent > 0},
		{"max retries", c.MaxRetries > 0},
		{"poll interval", c.PollInterval > 0},
		{"retry backoff", c.RetryBackoff > 0},
		{"cleanup after", c.CleanupAfter > 0},
		{"cleanup interval", c.CleanupInterval > 0},
		{"stale sending sweep interval", c.StaleSendingSweepInterval > 0},
		{"stale sending sweep limit", c.StaleSendingSweepLimit > 0},
	}

	var invalid []string

	for _, setting := range required {
		if !setting.valid {
			invalid = append(invalid, setting.name)
		}
	}

	if len(invalid) > 0 {
		return fmt.Errorf("settings must be positive: %s", strings.Join(invalid, ", "))
	}

	if err := workerconfig.ValidateNotificationDeliveryTiming(c.AttemptTimeout, c.StaleSendingAfter); err != nil {
		return fmt.Errorf("validate delivery timing: %w", err)
	}

	return nil
}

// Run은 claim·발송 loop와 유지보수 loop를 caller goroutine 아래에서 따로 실행하고 둘 다 끝날 때까지 기다린다.
// 유지보수(stale SENDING 격리, FAILED 집계, 보존 정리)가 느리거나 멈춰도 첫 claim과 이후 poll은 막히지 않는다.
// 한 loop가 panic으로 끝나면 다른 loop도 취소하고 모두 join한 뒤 panic 오류를 lifecycle owner에 반환한다.
func (d *Dispatcher) Run(ctx context.Context) error {
	if d == nil {
		return nil
	}

	loopCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	var (
		loops          sync.WaitGroup
		sendErr        error
		maintenanceErr error
	)

	loops.Go(func() {
		sendErr = d.runGuardedLoop(loopCtx, cancel, "delivery-dispatcher", d.runSendLoop)
	})
	loops.Go(func() {
		maintenanceErr = d.runGuardedLoop(loopCtx, cancel, "delivery-maintenance", d.runMaintenanceLoop)
	})
	loops.Wait()

	d.logger.Info("Delivery dispatcher stopped")

	if err := errors.Join(sendErr, maintenanceErr); err != nil {
		return fmt.Errorf("run delivery dispatcher: %w", err)
	}

	return nil
}

// runGuardedLoop은 어느 loop든 끝나면 다른 loop도 멈추게 한다. 한쪽만 남아 발송 없이 유지보수만 하거나
// 유지보수 없이 발송만 계속하는 반쪽 실행을 남기지 않기 위해서다.
func (d *Dispatcher) runGuardedLoop(ctx context.Context, cancel context.CancelCauseFunc, name string, loop func(context.Context)) error {
	err := panicguard.RunE(d.logger, panicscope.BackgroundTask, name, func() error {
		loop(ctx)

		return nil
	})
	if err != nil {
		err = fmt.Errorf("%s loop: %w", name, err)
	}

	cancel(err)

	return err
}

func (d *Dispatcher) runSendLoop(ctx context.Context) {
	d.processReady(ctx)

	if err := lifecycle.RunTickerLoop(ctx, d.config.PollInterval, func(ctx context.Context) error {
		d.processReady(ctx)

		return nil
	}); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		d.logger.Warn("Delivery dispatcher ticker stopped with error", slog.Any("error", err))
	}
}

func (d *Dispatcher) processItem(ctx context.Context, item *domain.NotificationDeliveryOutbox) {
	if d == nil || item == nil {
		return
	}

	if ctx.Err() != nil {
		return
	}

	var p deliverypayload.Payload

	if err := jsonv2.Unmarshal([]byte(item.Payload), &p); err != nil {
		d.markItemFailed(ctx, item, "payload unmarshal: "+err.Error())

		return
	}

	request, prepared := d.prepareRequest(ctx, item, p)
	if !prepared {
		return
	}

	if !d.markItemSending(ctx, item) {
		return
	}

	// 전송 직전 취소는 부수효과가 없으므로 일반 실패로 회수할 수 있습니다.
	if ctx.Err() != nil {
		finalCtx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(context.WithoutCancel(ctx)), workerconfig.NotificationDeliveryFinalizeTimeout)
		defer cancel()

		d.markItemFailed(finalCtx, item, ctx.Err().Error())

		return
	}

	irisRequestID, err := d.attemptSend(ctx, item, request)
	d.finishSend(ctx, item, request, irisRequestID, err)
}

// attemptSend는 외부 발송 한 번을 attempt로 기록하고, 성공이면 Iris가 admission을 증명한 requestId를 돌려준다.
func (d *Dispatcher) attemptSend(ctx context.Context, item *domain.NotificationDeliveryOutbox, request *deliverypayload.Request) (string, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, d.config.AttemptTimeout)

	defer cancel()

	var attemptID uint64

	if d.workerTracker != nil {
		attemptID = d.workerTracker.BeginAttempt(time.Now())
	}

	outcome := workercontract.AttemptPanic

	defer func() {
		if d.workerTracker != nil {
			d.workerTracker.EndAttempt(attemptID)
		}

		if d.workerTotals != nil {
			d.workerTotals.RecordAttempt(outcome)
		}
	}()

	irisRequestID, err := d.sendPrepared(attemptCtx, item, request)

	outcome = deliveryProviderOutcome(err)

	return irisRequestID, err
}

func (d *Dispatcher) finishSend(ctx context.Context, item *domain.NotificationDeliveryOutbox, request *deliverypayload.Request, irisRequestID string, err error) {
	// 부모 취소 뒤에도 결과를 저장하되 예산을 제한합니다. 저장 실패는 SENDING으로 남겨 sweep이 격리합니다.
	finalCtx, finalizeCancel := context.WithTimeout(pgxdb.WithControlAcquire(context.WithoutCancel(ctx)), workerconfig.NotificationDeliveryFinalizeTimeout)
	defer finalizeCancel()

	if err == nil {
		d.markItemSent(finalCtx, item, irisRequestID)

		return
	}

	if sendoutcome.Classify(err) == sendoutcome.Failed && iris.IsPreHandoffClientRequestIDConflict(err) {
		d.reissueRequest(finalCtx, item, request, err)

		return
	}

	d.logger.Error("Failed to send outbox message", slog.Int64("id", item.ID), privacylog.RoomIDAttr(item.RoomID), slog.Any("error", err))

	switch sendoutcome.Classify(err) {
	case sendoutcome.OutcomeUnknown, sendoutcome.TransportAmbiguous:
		d.markItemQuarantined(finalCtx, item, err.Error())
	case sendoutcome.Failed:
		d.markItemFailed(finalCtx, item, err.Error())
	case sendoutcome.Success:
	}
}

func (d *Dispatcher) markItemQuarantined(ctx context.Context, item *domain.NotificationDeliveryOutbox, reason string) {
	ctx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(ctx), workerconfig.NotificationDeliveryFinalizeTimeout)
	defer cancel()

	if err := d.repository.MarkQuarantined(ctx, item.ID, d.workerID, item.AttemptCount, reason); err != nil {
		d.reportTransitionError(item.ID, err, "Failed to quarantine outbox item")
	}
}

func deliveryAttemptFailure(err error) workercontract.AttemptOutcome {
	if errors.Is(err, context.DeadlineExceeded) {
		return workercontract.AttemptTimeout
	}

	if errors.Is(err, context.Canceled) {
		return workercontract.AttemptCanceled
	}

	return workercontract.AttemptFailed
}

// markItemSending은 발송 전 claim을 SENDING으로 옮기고 외부 발송을 시작해도 되는지 보고한다. 미스가 난 행은 현재
// 소유자·회수 경로의 것이므로 발송하지 않는다.
func (d *Dispatcher) markItemSending(ctx context.Context, item *domain.NotificationDeliveryOutbox) bool {
	ctx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(ctx), workerconfig.NotificationDeliveryFinalizeTimeout)
	defer cancel()

	if err := d.repository.MarkSending(ctx, item.ID, d.workerID, item.AttemptCount, workerconfig.NotificationDeliveryLease); err != nil {
		d.reportTransitionError(item.ID, err, "Failed to mark outbox item sending")

		return false
	}

	return true
}

// markItemSent는 발송 성공과 Iris requestId를 함께 기록해 handoff 확인을 예약한다. 지평은 handoff reconciler와 같은
// attempt timeout이다.
func (d *Dispatcher) markItemSent(ctx context.Context, item *domain.NotificationDeliveryOutbox, irisRequestID string) {
	if err := d.repository.MarkSent(ctx, item.ID, d.workerID, item.AttemptCount, irisRequestID, d.config.AttemptTimeout); err != nil {
		d.reportTransitionError(item.ID, err, "Failed to mark outbox item as sent")
	}
}

func (d *Dispatcher) markItemFailed(ctx context.Context, item *domain.NotificationDeliveryOutbox, reason string) {
	ctx, cancel := context.WithTimeout(pgxdb.WithControlAcquire(ctx), workerconfig.NotificationDeliveryFinalizeTimeout)
	defer cancel()

	if err := d.repository.MarkFailed(ctx, item.ID, d.workerID, item.AttemptCount, d.config.MaxRetries, d.config.RetryBackoff, reason); err != nil {
		d.reportTransitionError(item.ID, err, "Failed to mark outbox item failed")
	}
}

// reportTransitionError는 소유자 전이 오류를 남긴다. 미스가 난 행은 현재 소유자·회수 경로에 맡기고 지표와 로그만
// 남기며, 그 밖의 오류는 commit 여부를 모르는 DB 오류로 기록한다.
func (d *Dispatcher) reportTransitionError(id int64, err error, message string) {
	if miss, ok := errors.AsType[*sendfence.MissError](err); ok {
		d.recordFenceMiss(id, miss, fenceMissResultFenceLost, nil)

		return
	}

	d.logger.Error(message, slog.Int64("id", id), slog.Any("error", err))
}

// recordFenceMiss는 미스 행마다 지표를 올리고 로그를 남긴다. 발송 성공을 기록하지 못한 mark_sent 미스는 외부 결과가
// 원장에 남지 않으므로 오류로, 그 밖의 미스는 경고로 남긴다.
func (d *Dispatcher) recordFenceMiss(id int64, miss *sendfence.MissError, result string, transitionErr error) {
	for _, row := range miss.Rows {
		notificationDeliveryFenceMissTotal.WithLabelValues(miss.Action, string(row.Reason), result).Inc()

		attrs := []any{
			slog.Int64("id", id), slog.String("action", miss.Action), slog.String("reason", string(row.Reason)),
			slog.String("observed_status", row.Status), slog.String("result", result),
		}

		switch {
		case transitionErr != nil:
			d.logger.Error("Failed to settle delivery fence miss", append(attrs, slog.Any("error", transitionErr))...)
		case result == fenceMissResultFailed:
			d.logger.Warn("Delivery fence miss settled as failure", attrs...)
		case miss.Action == actionMarkSent:
			d.logger.Error("Delivery send succeeded but fence lost before recording it; row left to its current owner", attrs...)
		default:
			d.logger.Warn("Delivery fence lost; row left to its current owner", attrs...)
		}
	}
}

// 이 결정적 ID는 Iris reply admission의 멱등 키다. 발송 전 claim만 lease(NotificationDeliveryLease) 만료로
// 회수되고 결과 불명 발송과 stale SENDING은 격리되므로, 이 경로의 lease는 Iris admission 보존 기간과
// 묶이지 않는다. Iris 자동 재발송 지평에 대한 lease 회수 지연 검사는 alarm_dispatch 프로필 검증이 소유한다.
func notificationDeliveryClientRequestID(item *domain.NotificationDeliveryOutbox) string {
	kind := ""
	contentID := ""
	roomID := ""

	if item != nil {
		kind = string(item.Kind)
		contentID = item.ContentID
		roomID = item.RoomID
	}

	if contentID == "" && item != nil {
		contentID = strconv.FormatInt(item.ID, 10)
	}

	sum := sha256.Sum256([]byte(kind + "\x00" + contentID + "\x00" + roomID))

	return "hololive-delivery:" + hex.EncodeToString(sum[:16])
}

func deliveryProviderOutcome(err error) workercontract.AttemptOutcome {
	if err == nil {
		return workercontract.AttemptSuccess
	}

	outcome := deliveryAttemptFailure(err)
	if outcome != workercontract.AttemptFailed {
		return outcome
	}

	switch sendoutcome.Classify(err) {
	case sendoutcome.OutcomeUnknown, sendoutcome.TransportAmbiguous:
		return workercontract.AttemptOutcomeUnknown
	case sendoutcome.Success, sendoutcome.Failed:
		return outcome
	}

	return outcome
}
