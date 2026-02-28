package valkeyx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"
)

const (
	defaultClaimMinIdle         = 60 * time.Second
	defaultClaimInterval        = 30 * time.Second
	defaultClaimBatchSize       = 10
	defaultMaxDelivery          = 5
	defaultDLQMaxLen      int64 = 1000
	maxDrainIterations          = 100
)

type Clients struct {
	General  valkey.Client
	Blocking valkey.Client
}

type ClientConfig struct {
	InitAddress []string
	Username    string
	Password    string //nolint:gosec // 설정 구조체 필드명이며 시크릿 값 자체를 로그/출력하지 않는다.
	ClientName  string
	SelectDB    int
}

func NewClients(cfg ClientConfig) (*Clients, error) {
	general, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: cfg.InitAddress,
		Username:    cfg.Username,
		Password:    cfg.Password,
		ClientName:  cfg.ClientName,
		SelectDB:    cfg.SelectDB,
	})
	if err != nil {
		return nil, fmt.Errorf("valkey general client: %w", err)
	}

	blocking, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: cfg.InitAddress,
		Username:    cfg.Username,
		Password:    cfg.Password,
		ClientName:  cfg.ClientName + "-streams",
		SelectDB:    cfg.SelectDB,
	})
	if err != nil {
		general.Close()
		return nil, fmt.Errorf("valkey blocking client: %w", err)
	}

	return &Clients{General: general, Blocking: blocking}, nil
}

func (c *Clients) Close() {
	if c == nil {
		return
	}
	if c.General != nil {
		c.General.Close()
	}
	if c.Blocking != nil {
		c.Blocking.Close()
	}
}

type StreamMessage struct {
	ID     string
	Values map[string]string
}

type StreamHandler func(ctx context.Context, msg StreamMessage) error

type handlerFailure struct {
	MessageID string
	Err       error
}

type StreamConsumer struct {
	Log      *slog.Logger
	General  valkey.Client
	Blocking valkey.Client

	Stream   string
	Group    string
	Consumer string

	Count        int64
	BlockTimeout time.Duration

	AckAfterHandle bool

	WorkerPool *workerpool.Pool

	// Pending recovery 설정 (AckAfterHandle=true 시 자동 활성화)
	ClaimMinIdle     time.Duration // XAUTOCLAIM min-idle-time (기본: 60s)
	ClaimInterval    time.Duration // XAUTOCLAIM 실행 주기 (기본: 30s)
	ClaimBatchSize   int64         // XAUTOCLAIM COUNT (기본: 10)
	MaxDeliveryCount int64         // DLQ 이동 임계값 (기본: 5)
	DLQStream        string        // DLQ 스트림 이름 (기본: "{stream}:dlq")
	DLQMaxLen        int64         // DLQ 스트림 최대 길이 (기본: 1000)
}

func (c StreamConsumer) Run(ctx context.Context, handler StreamHandler) error {
	if handler == nil {
		return errors.New("handler is nil")
	}
	if c.General == nil || c.Blocking == nil {
		return errors.New("valkey client is nil")
	}
	c.applyDefaults()
	if !c.AckAfterHandle {
		c.Log.Warn(
			"ack after handle disabled; caller must ack manually",
			"stream", c.Stream,
			"consumer_group", c.Group,
			"consumer", c.Consumer,
		)
	}

	blockMs := int64(c.BlockTimeout / time.Millisecond)
	if blockMs <= 0 {
		blockMs = 1000
	}

	// 시작 시 자기 pending 메시지 소화
	if c.AckAfterHandle {
		c.drainPending(ctx, handler)
	}

	dc, release := c.Blocking.Dedicate()
	defer release()

	// XAUTOCLAIM 주기적 실행 ticker
	var claimTicker *time.Ticker
	var claimCh <-chan time.Time
	if c.AckAfterHandle {
		claimTicker = time.NewTicker(c.ClaimInterval)
		defer claimTicker.Stop()
		claimCh = claimTicker.C
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("stream consumer context canceled: %w", err)
		}

		// 주기적 XAUTOCLAIM (non-blocking 체크)
		select {
		case <-claimCh:
			c.claimStale(ctx, handler)
		default:
		}

		cmd := dc.B().
			Xreadgroup().
			Group(c.Group, c.Consumer).
			Count(c.Count).
			Block(blockMs).
			Streams().
			Key(c.Stream).Id(">").
			Build()

		res := dc.Do(ctx, cmd)
		if err := res.Error(); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return fmt.Errorf("stream read interrupted: %w", err)
			}
			c.Log.Warn("valkey xreadgroup failed", "err", err)
			time.Sleep(300 * time.Millisecond)
			continue
		}

		xrange, err := res.AsXRead()
		if err != nil {
			if valkey.IsValkeyNil(err) {
				continue
			}
			c.Log.Warn("valkey xreadgroup decode failed", "err", err)
			continue
		}

		entries, ok := xrange[c.Stream]
		if !ok || len(entries) == 0 {
			continue
		}

		if c.WorkerPool != nil {
			c.processWithPool(ctx, handler, entries)
		} else {
			c.processSequential(ctx, handler, entries)
		}
	}
}

// applyDefaults: 설정 기본값 적용
func (c *StreamConsumer) applyDefaults() {
	if c.Log == nil {
		c.Log = slog.Default()
	}
	if c.Count <= 0 {
		c.Count = 10
	}
	if c.BlockTimeout <= 0 {
		c.BlockTimeout = 5 * time.Second
	}
	if c.AckAfterHandle {
		if c.ClaimMinIdle <= 0 {
			c.ClaimMinIdle = defaultClaimMinIdle
		}
		if c.ClaimInterval <= 0 {
			c.ClaimInterval = defaultClaimInterval
		}
		if c.ClaimBatchSize <= 0 {
			c.ClaimBatchSize = defaultClaimBatchSize
		}
		if c.MaxDeliveryCount <= 0 {
			c.MaxDeliveryCount = defaultMaxDelivery
		}
		if c.DLQStream == "" {
			c.DLQStream = c.Stream + ":dlq"
		}
		if c.DLQMaxLen <= 0 {
			c.DLQMaxLen = defaultDLQMaxLen
		}
	}
}

// drainPending: 시작 시 자기 consumer의 미ACK 메시지를 우선 소화
func (c StreamConsumer) drainPending(ctx context.Context, handler StreamHandler) {
	c.Log.Info("draining pending messages", "stream", c.Stream, "consumer", c.Consumer)
	for range maxDrainIterations {
		if ctx.Err() != nil {
			return
		}

		// Id("0"): 자기 consumer의 pending 메시지 반환
		cmd := c.General.B().
			Xreadgroup().
			Group(c.Group, c.Consumer).
			Count(c.Count).
			Streams().
			Key(c.Stream).Id("0").
			Build()

		res := c.General.Do(ctx, cmd)
		xrange, err := res.AsXRead()
		if err != nil {
			if valkey.IsValkeyNil(err) {
				break
			}
			c.Log.Warn("drain pending read failed", "err", err)
			break
		}

		entries, ok := xrange[c.Stream]
		if !ok || len(entries) == 0 {
			break
		}

		c.Log.Info("processing pending messages", "stream", c.Stream, "count", len(entries))
		if c.WorkerPool != nil {
			c.processWithPool(ctx, handler, entries)
		} else {
			c.processSequential(ctx, handler, entries)
		}
	}
	c.Log.Info("drain pending complete", "stream", c.Stream)
}

// claimStale: 다른 consumer의 stale pending 메시지를 XAUTOCLAIM으로 claim
func (c StreamConsumer) claimStale(ctx context.Context, handler StreamHandler) {
	minIdleMs := strconv.FormatInt(c.ClaimMinIdle.Milliseconds(), 10)
	startID := "0-0"

	for {
		if ctx.Err() != nil {
			return
		}

		cmd := c.General.B().
			Xautoclaim().
			Key(c.Stream).
			Group(c.Group).
			Consumer(c.Consumer).
			MinIdleTime(minIdleMs).
			Start(startID).
			Count(c.ClaimBatchSize).
			Build()

		result, err := ParseXAutoClaim(c.General.Do(ctx, cmd))
		if err != nil {
			c.Log.Warn("xautoclaim failed", "err", err, "stream", c.Stream)
			return
		}

		if len(result.Entries) == 0 {
			return
		}

		// delivery count 확인 후 DLQ 이동 또는 재처리
		c.processClaimedEntries(ctx, handler, result.Entries)

		// 커서가 "0-0"이면 전체 PEL 스캔 완료
		if result.NextStartID == "0-0" {
			return
		}
		startID = result.NextStartID
	}
}

// processClaimedEntries: claim된 메시지의 delivery count 확인 후 처리
func (c StreamConsumer) processClaimedEntries(ctx context.Context, handler StreamHandler, entries []valkey.XRangeEntry) {
	if len(entries) == 0 {
		return
	}

	// XPENDING으로 delivery count 배치 조회
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}

	deliveryCounts := c.getDeliveryCounts(ctx, ids)
	ackIDs := make([]string, 0, len(entries))
	handlerFailures := make([]handlerFailure, 0, len(entries))

	for _, entry := range entries {
		if ctx.Err() != nil {
			return
		}

		count := deliveryCounts[entry.ID]
		if count >= c.MaxDeliveryCount {
			c.Log.Warn("moving to dlq", "stream", c.Stream, "id", entry.ID, "delivery_count", count)
			if dlqErr := MoveToDLQ(ctx, c.General, c.Stream, c.Group, c.DLQStream, entry, count, c.DLQMaxLen); dlqErr != nil {
				c.Log.Warn("dlq move failed", "err", dlqErr, "stream", c.Stream, "id", entry.ID)
			}
			continue
		}

		// 정상 재처리
		msg := StreamMessage{ID: entry.ID, Values: entry.FieldValues}
		if handleErr := handler(ctx, msg); handleErr != nil {
			if c.AckAfterHandle {
				c.Log.Warn("claimed message handler failed", "err", handleErr, "stream", c.Stream, "id", entry.ID)
			} else {
				handlerFailures = append(handlerFailures, handlerFailure{MessageID: entry.ID, Err: handleErr})
			}
			continue
		}
		if c.AckAfterHandle {
			ackIDs = append(ackIDs, entry.ID)
		}
	}

	if !c.AckAfterHandle {
		c.logAckDisabledHandlerFailures("claimed message handler failed", handlerFailures)
		return
	}

	if len(ackIDs) == 0 {
		return
	}

	if ackErr := c.ackAndDelete(ctx, ackIDs); ackErr != nil {
		c.Log.Warn("claimed message ack failed", "err", ackErr, "stream", c.Stream, "count", len(ackIDs))
	}
}

// getDeliveryCounts: XPENDING으로 지정 메시지 ID들의 delivery count 조회
func (c StreamConsumer) getDeliveryCounts(ctx context.Context, ids []string) map[string]int64 {
	result := make(map[string]int64, len(ids))
	if len(ids) == 0 {
		return result
	}

	// XPENDING range로 해당 ID 범위 조회 (첫 ID ~ 마지막 ID)
	cmd := c.General.B().
		Xpending().
		Key(c.Stream).
		Group(c.Group).
		Start(ids[0]).
		End(ids[len(ids)-1]).
		Count(int64(len(ids))).
		Build()

	entries, err := ParseXPendingEntries(c.General.Do(ctx, cmd))
	if err != nil {
		c.Log.Warn("xpending failed", "err", err, "stream", c.Stream)
		return result
	}

	for _, e := range entries {
		result[e.ID] = e.DeliveryCount
	}
	return result
}

func (c StreamConsumer) processSequential(ctx context.Context, handler StreamHandler, entries []valkey.XRangeEntry) {
	ackIDs := make([]string, 0, len(entries))
	handlerFailures := make([]handlerFailure, 0, len(entries))
	for _, entry := range entries {
		msg := StreamMessage{
			ID:     entry.ID,
			Values: entry.FieldValues,
		}
		if err := handler(ctx, msg); err != nil {
			if c.AckAfterHandle {
				c.Log.Warn("stream handler failed", "err", err, "stream", c.Stream, "id", msg.ID)
			} else {
				handlerFailures = append(handlerFailures, handlerFailure{MessageID: msg.ID, Err: err})
			}
			continue
		}
		if c.AckAfterHandle {
			ackIDs = append(ackIDs, entry.ID)
		}
	}

	if !c.AckAfterHandle {
		c.logAckDisabledHandlerFailures("stream handler failed", handlerFailures)
		return
	}

	if len(ackIDs) == 0 {
		return
	}

	if err := c.ackAndDelete(ctx, ackIDs); err != nil {
		c.Log.Warn("valkey ack pipeline failed", "err", err, "stream", c.Stream)
	}
}

func (c StreamConsumer) processWithPool(ctx context.Context, handler StreamHandler, entries []valkey.XRangeEntry) {
	// 채널 기반 결과 수집: 각 워커가 성공 시 ID를 전송
	ackCh := make(chan string, len(entries))
	handlerErrCh := make(chan handlerFailure, len(entries))
	var wg sync.WaitGroup

	for _, entry := range entries {
		wg.Add(1)
		err := c.WorkerPool.Submit(func() {
			defer wg.Done()
			msg := StreamMessage{
				ID:     entry.ID,
				Values: entry.FieldValues,
			}
			if err := handler(ctx, msg); err != nil {
				if c.AckAfterHandle {
					c.Log.Warn("stream handler failed", "err", err, "stream", c.Stream, "id", msg.ID)
				} else {
					handlerErrCh <- handlerFailure{MessageID: msg.ID, Err: err}
				}
				return
			}
			if c.AckAfterHandle {
				ackCh <- entry.ID
			}
		})
		if err != nil {
			wg.Done()
			c.Log.Warn("worker pool submit failed", "err", err, "stream", c.Stream, "id", entry.ID)
		}
	}

	// 별도 goroutine으로 채널 닫기 관리
	go func() {
		wg.Wait()
		close(ackCh)
		close(handlerErrCh)
	}()

	// 채널에서 ACK ID 수집 (blocking 없이 순차 처리)
	ackIDs := make([]string, 0, len(entries))
	for id := range ackCh {
		ackIDs = append(ackIDs, id)
	}

	handlerFailures := make([]handlerFailure, 0, len(entries))
	for failure := range handlerErrCh {
		handlerFailures = append(handlerFailures, failure)
	}

	if !c.AckAfterHandle {
		c.logAckDisabledHandlerFailures("stream handler failed", handlerFailures)
		return
	}

	if len(ackIDs) == 0 {
		return
	}

	if err := c.ackAndDelete(ctx, ackIDs); err != nil {
		c.Log.Warn("valkey ack pipeline failed", "err", err, "stream", c.Stream)
	}
}

func (c StreamConsumer) ackAndDelete(ctx context.Context, ackIDs []string) error {
	if len(ackIDs) == 0 {
		return nil
	}

	logger := c.Log
	if logger == nil {
		logger = slog.Default()
	}
	if !c.AckAfterHandle {
		logger.Warn(
			"ack/delete skipped because ack after handle is disabled",
			"stream", c.Stream,
			"group", c.Group,
			"consumer", c.Consumer,
			"count", len(ackIDs),
		)
		return nil
	}

	cmds := valkey.Commands{
		c.General.B().Xack().Key(c.Stream).Group(c.Group).Id(ackIDs...).Build(),
		c.General.B().Xdel().Key(c.Stream).Id(ackIDs...).Build(),
	}

	results := c.General.DoMulti(ctx, cmds...)
	if len(results) != len(cmds) {
		logger.Warn(
			"stream ack pipeline result length mismatch",
			"stream", c.Stream,
			"group", c.Group,
			"consumer", c.Consumer,
			"expected", len(cmds),
			"actual", len(results),
		)
	}

	for i, r := range results {
		if err := r.Error(); err != nil {
			return fmt.Errorf("failed to ack/delete stream messages[%d]: %w", i, err)
		}
	}

	expected := int64(len(ackIDs))

	if len(results) >= 1 {
		acked, err := results[0].AsInt64()
		if err != nil {
			return fmt.Errorf("parse xack affected count: %w", err)
		}
		if acked != expected {
			logger.Warn(
				"stream xack affected count mismatch",
				"stream", c.Stream,
				"group", c.Group,
				"consumer", c.Consumer,
				"expected", expected,
				"actual", acked,
			)
		}
	}

	if len(results) >= 2 {
		deleted, err := results[1].AsInt64()
		if err != nil {
			return fmt.Errorf("parse xdel affected count: %w", err)
		}
		if deleted != expected {
			logger.Warn(
				"stream xdel affected count mismatch",
				"stream", c.Stream,
				"group", c.Group,
				"consumer", c.Consumer,
				"expected", expected,
				"actual", deleted,
			)
		}
	}

	return nil
}

func (c StreamConsumer) AckAnd(ctx context.Context, ackIDs []string, extra ...valkey.Completed) error {
	if len(ackIDs) == 0 {
		return nil
	}

	cmds := make(valkey.Commands, 0, 1+len(extra))
	cmds = append(cmds, c.General.B().Xack().Key(c.Stream).Group(c.Group).Id(ackIDs...).Build())
	cmds = append(cmds, extra...)

	results := c.General.DoMulti(ctx, cmds...)
	for _, r := range results {
		if err := r.Error(); err != nil {
			return fmt.Errorf("failed to ack stream messages: %w", err)
		}
	}
	return nil
}

func (c StreamConsumer) logAckDisabledHandlerFailures(message string, failures []handlerFailure) {
	if len(failures) == 0 {
		return
	}

	logger := c.Log
	if logger == nil {
		logger = slog.Default()
	}

	logger.Warn(
		"handler failures while ack after handle is disabled",
		"stream", c.Stream,
		"consumer_group", c.Group,
		"context", message,
		"error_count", len(failures),
	)
	for _, failure := range failures {
		logger.Warn(
			message,
			"stream", c.Stream,
			"consumer_group", c.Group,
			"message_id", failure.MessageID,
			"error", failure.Err,
		)
	}
}
