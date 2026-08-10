package workerapp

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-alarm-worker/internal/service/dispatchrun"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
	"github.com/park285/iris-client-go/iris"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NewIrisMessageSender가 any를 받아 type switch로 판별하므로, 이 단언이 없으면 SDK 시그니처가
// 어긋나도 컴파일이 통과하고 런타임에 client=nil sender로 조용히 전락한다.
var _ egress.IrisClient = (*delivery.RuntimeIrisClient)(nil)

type youtubeOutboxKaringCapableSender interface {
	SendYouTubeOutboxKaring(ctx context.Context, roomID string, payload *domain.YouTubeOutboxDispatchPayload) error
}

type clientRequestIDRecordingIrisSender struct {
	roomID       string
	message      string
	opts         int
	markdownSent bool
}

func (s *clientRequestIDRecordingIrisSender) SendMessage(_ context.Context, roomID, message string, opts ...iris.SendOption) error {
	s.roomID = roomID
	s.message = message
	s.opts = len(opts)
	return nil
}

func (s *clientRequestIDRecordingIrisSender) SendMarkdown(_ context.Context, roomID, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	s.roomID = roomID
	s.message = markdown
	s.opts = len(opts)
	s.markdownSent = true
	return &iris.ReplyAcceptedResponse{}, nil
}

func (*clientRequestIDRecordingIrisSender) SendKaringContentList(context.Context, *iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error) {
	return nil, nil
}

type workerappEgressTestPostgres struct{}

func (workerappEgressTestPostgres) GetPool() *pgxpool.Pool {
	return nil
}

func (workerappEgressTestPostgres) Ping(context.Context) error {
	return nil
}

func (workerappEgressTestPostgres) Close() error {
	return nil
}

func TestBuildYouTubeOutboxSenderDisablesKaringByDefault(t *testing.T) {
	t.Setenv("YOUTUBE_OUTBOX_KARING_ENABLED", "")
	irisSender := egress.NewIrisMessageSender(nil)

	sender := buildYouTubeOutboxSender(irisSender, nil)

	assert.Same(t, irisSender, sender)
	_, ok := sender.(youtubeOutboxKaringCapableSender)
	assert.False(t, ok)
}

func TestBuildYouTubeOutboxSenderEnablesKaringWhenConfigured(t *testing.T) {
	t.Setenv("YOUTUBE_OUTBOX_KARING_ENABLED", "true")
	irisSender := egress.NewIrisMessageSender(nil)

	sender := buildYouTubeOutboxSender(irisSender, nil)

	_, ok := sender.(youtubeOutboxKaringCapableSender)
	assert.True(t, ok)
}

func TestYouTubeOutboxKaringSenderPreservesClientRequestIDOptionThroughEgress(t *testing.T) {
	stub := &clientRequestIDRecordingIrisSender{}
	sender := dispatchrun.NewYouTubeOutboxKaringSender(egress.NewIrisMessageSender(stub), nil)

	require.NoError(t, sender.SendMessageWithClientRequestID(t.Context(), "room-1", "hello", "req-1"))

	assert.Equal(t, "room-1", stub.roomID)
	assert.Equal(t, "hello", stub.message)
	assert.Equal(t, 1, stub.opts)
	assert.False(t, stub.markdownSent)
}

func TestYouTubeOutboxKaringSenderUsesMarkdownLaneWhenEnabled(t *testing.T) {
	stub := &clientRequestIDRecordingIrisSender{}
	sender := dispatchrun.NewYouTubeOutboxKaringSender(
		egress.NewIrisMessageSender(stub, egress.WithMarkdownReplies(true)),
		nil,
	)

	require.NoError(t, sender.SendMessageWithClientRequestID(t.Context(), "room-1", "**hello**", "req-1"))

	assert.True(t, stub.markdownSent)
	assert.Equal(t, "room-1", stub.roomID)
	assert.Equal(t, "**hello**", stub.message)
	assert.Equal(t, 1, stub.opts)
}

func TestBuildNotificationEgressRequiresPostgres(t *testing.T) {
	runner, err := buildNotificationEgress(&settings.Config{}, &sharedmodules.InfraModule{}, nil)

	require.Error(t, err)
	assert.Nil(t, runner)
	assert.Contains(t, err.Error(), "postgres is required")
}

func TestBuildAlarmDispatchRunnerBuildsPGRunner(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "true")
	t.Setenv("ALARM_DISPATCH_MAX_BATCH", "7")
	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "true")
	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	scheduler, err := buildAlarmDispatchRunner(infra, egress.NewIrisMessageSender(nil), nil)
	require.NoError(t, err)

	runner, ok := scheduler.(*dispatchrun.Runner)
	require.True(t, ok)
	assert.NotNil(t, runner)
	runnerConfig := alarmDispatchRunnerConfig()
	assert.Equal(t, 7, runnerConfig.MaxBatch)
	assert.True(t, runnerConfig.KaringEnabled)
}

func TestParseAlarmDispatchKaringEnabledFromEnv(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "")
	assert.False(t, parseAlarmDispatchKaringEnabled())

	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "true")
	assert.True(t, parseAlarmDispatchKaringEnabled())

	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "false")
	assert.False(t, parseAlarmDispatchKaringEnabled())
}

func TestBuildAlarmDispatchRunnerHonorsBatchEnv(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "true")
	t.Setenv("ALARM_DISPATCH_MAX_BATCH", "9")
	t.Setenv("ALARM_DISPATCH_MAX_BATCHES_PER_WAKE", "3")
	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "false")
	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	scheduler, err := buildAlarmDispatchRunner(infra, egress.NewIrisMessageSender(nil), nil)
	require.NoError(t, err)

	runner, ok := scheduler.(*dispatchrun.Runner)
	require.True(t, ok)
	assert.NotNil(t, runner)
	runnerConfig := alarmDispatchRunnerConfig()
	assert.Equal(t, 9, runnerConfig.MaxBatch)
	assert.Equal(t, 3, runnerConfig.MaxBatchesPerWake)
	assert.False(t, runnerConfig.KaringEnabled)
}

func TestBuildEgressDispatchersRespectDisabledFlags(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "false")
	t.Setenv("DELIVERY_DISPATCHER_ENABLED", "false")
	t.Setenv("YOUTUBE_OUTBOX_DISPATCHER_ENABLED", "false")

	scheduler, err := buildAlarmDispatchRunner(nil, egress.NewIrisMessageSender(nil), nil)
	require.NoError(t, err)
	assert.Nil(t, scheduler)

	scheduler, err = buildDeliveryOutboxDispatcher(nil, egress.NewIrisMessageSender(nil), nil)
	require.NoError(t, err)
	assert.Nil(t, scheduler)

	scheduler, err = buildYouTubeOutboxDispatcher(nil, egress.NewIrisMessageSender(nil), nil)
	require.NoError(t, err)
	assert.Nil(t, scheduler)
}

func TestBuildEgressDispatchersRejectMissingInfraWhenEnabled(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "true")
	t.Setenv("DELIVERY_DISPATCHER_ENABLED", "true")
	t.Setenv("YOUTUBE_OUTBOX_DISPATCHER_ENABLED", "true")

	scheduler, err := buildAlarmDispatchRunner(nil, egress.NewIrisMessageSender(nil), nil)
	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "infra is required")

	scheduler, err = buildDeliveryOutboxDispatcher(nil, egress.NewIrisMessageSender(nil), nil)
	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "postgres is required")

	scheduler, err = buildYouTubeOutboxDispatcher(nil, egress.NewIrisMessageSender(nil), nil)
	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "postgres is required")
}

func TestBuildYouTubeOutboxDispatcherValidatesV3HandoffActivation(t *testing.T) {
	t.Setenv("YOUTUBE_OUTBOX_DISPATCHER_ENABLED", "false")
	t.Setenv("YOUTUBE_OUTBOX_V3_HANDOFF_MODE", "shadow")

	scheduler, err := buildYouTubeOutboxDispatcher(nil, egress.NewIrisMessageSender(nil), nil)
	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "requires YOUTUBE_OUTBOX_DISPATCHER_ENABLED=true")

	t.Setenv("YOUTUBE_OUTBOX_DISPATCHER_ENABLED", "true")
	t.Setenv("YOUTUBE_OUTBOX_V3_HANDOFF_MODE", "cutover")
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "false")
	scheduler, err = buildYouTubeOutboxDispatcher(
		&sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}},
		egress.NewIrisMessageSender(nil),
		nil,
	)
	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "requires ALARM_DISPATCH_CONSUMER_ENABLED=true")

	t.Setenv("YOUTUBE_OUTBOX_V3_HANDOFF_MODE", "dual-write")
	scheduler, err = buildYouTubeOutboxDispatcher(
		&sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}},
		egress.NewIrisMessageSender(nil),
		nil,
	)
	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "unsupported mode")
}

type claimKeyReleaseRecordingCache struct {
	cache.Client
	delManyCalls int
	delManyKeys  []string
}

func (c *claimKeyReleaseRecordingCache) DelMany(_ context.Context, keys []string) (int64, error) {
	c.delManyCalls++
	c.delManyKeys = append(c.delManyKeys, keys...)
	return int64(len(keys)), nil
}

func TestNewAlarmDispatchConsumerWiresPGModeClaimKeyReleaser(t *testing.T) {
	cacheFake := &claimKeyReleaseRecordingCache{}
	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}, Cache: cacheFake}
	consumer := newAlarmDispatchConsumer(infra, nil)

	err := consumer.ReleaseClaimKeys(context.Background(), []string{
		"notified:claim:room-1:stream-1:100:live",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, cacheFake.delManyCalls,
		"pg-mode consumer must wire claim-key releaser so DLQ claim keys are released (H14/P1-d)")
	assert.Equal(t, []string{"notified:claim:room-1:stream-1:100:live"}, cacheFake.delManyKeys)
}
