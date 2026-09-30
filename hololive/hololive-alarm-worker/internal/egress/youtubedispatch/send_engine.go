package youtubedispatch

import (
	"context"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/workercontract"

	ytlifecycle "github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	dispatchstate "github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
	messagedelivery "github.com/kapu/hololive-shared/pkg/service/delivery"
)

type SendEngine struct {
	workerTracker   *workercontract.ExecutorTracker
	workerTotals    *workercontract.Counters
	sender          messagedelivery.MessageSender
	formatter       *MessageFormatter
	logger          *slog.Logger
	config          dispatchstate.Config
	claims          ClaimResolver
	auditLogger     *AuditLogger
	metricsRecorder *MetricsRecorder
	transition      deliveryTransition
}

// deliveryTransition의 실패·완료 전이는 같은 트랜잭션 안에서 시도 telemetry도 기록한다. 호출자는 발송 방식만 넘긴다
// (DEC-20260926-hololive-delivery-telemetry-single-path).
type deliveryTransition interface {
	requestTransition
	lifecycleTransition
}

type requestTransition interface {
	FreezeFallbackRequests(context.Context, store.StartedOperation, []store.FrozenRequest) ([]store.FrozenRequest, error)
	LoadFrozenRequests(context.Context, []int64) ([]store.FrozenRequest, error)
	FreezeRequest(context.Context, []domain.YouTubeNotificationDelivery, store.FrozenRequest) (store.FrozenRequest, error)
	AdvanceRequestGeneration(context.Context, store.StartedOperation, store.FrozenRequest) (store.FrozenRequest, error)
	DeferFollower(context.Context, store.DeferCommand) (store.ApplyResult, error)
}

type lifecycleTransition interface {
	PrepareClaimed(context.Context, []domain.YouTubeNotificationDelivery, map[int64]domain.YouTubeNotificationOutbox) (store.PrepareClaimsResult, error)
	BeginSending(context.Context, []domain.YouTubeNotificationDelivery, map[int64]domain.YouTubeNotificationOutbox) (store.StartedOperation, store.ApplyResult, error)
	ApplyPreparedFailure(context.Context, []domain.YouTubeNotificationDelivery, map[int64]domain.YouTubeNotificationOutbox, ytlifecycle.FailureKind, ytlifecycle.Reason, time.Duration, store.DeliveryMode) (store.ApplyResult, error)
	ApplyStartedFailure(context.Context, store.StartedOperation, ytlifecycle.FailureKind, ytlifecycle.Reason, time.Duration, store.DeliveryMode) (store.ApplyResult, error)
	CompleteSent(context.Context, store.StartedOperation, []dispatchstate.ClaimToken, store.DeliveryMode) (store.ApplyResult, error)
}

func newSendEngine(
	sender messagedelivery.MessageSender,
	formatter *MessageFormatter,
	logger *slog.Logger,
	config *dispatchstate.Config,
	claims ClaimResolver,
	auditLogger *AuditLogger,
	metricsRecorder *MetricsRecorder,
	transition deliveryTransition,
) *SendEngine {
	if transition == nil {
		panic("youtube send engine requires delivery transition")
	}

	if logger == nil {
		logger = slog.Default()
	}

	engine := &SendEngine{
		sender:          sender,
		formatter:       formatter,
		logger:          logger,
		config:          *config,
		claims:          claims,
		auditLogger:     auditLogger,
		metricsRecorder: metricsRecorder,
		transition:      transition,
	}

	return engine
}
