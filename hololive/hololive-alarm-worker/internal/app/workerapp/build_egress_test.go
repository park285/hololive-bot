package workerapp

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/iris-client-go/v2/iris"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	"github.com/kapu/hololive-alarm-worker/internal/service/dispatchrun"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings/alarmworker"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/alarm/handoff"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
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
	return &iris.KaringDryRunResponse{}, nil
}

type staticOpenRooms struct{}

func (staticOpenRooms) OpenChat(context.Context, string) bool { return true }

type workerappEgressTestPostgres struct{}

func alarmWorkerTestConfig(t *testing.T) (*settings.Config, *alarmWorkerRegistryState) {
	t.Helper()

	path, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "hololive-shared", "pkg", "config", "settings", "testdata", "stack-worker-profile-alarm-worker.json"))
	require.NoError(t, err)
	t.Setenv(workercontract.ProfileFileEnv, path)

	profile, err := alarmworker.LoadWorkerProfile()
	require.NoError(t, err)

	profile.AlarmDispatch.WakeupEnabled = false

	state := &alarmWorkerRegistryState{
		trackers: make(map[string]*workercontract.ExecutorTracker, 3),
		totals:   make(map[string]*workercontract.Counters, 3),
		workers:  profile.Loaded.Profile.Workers,
	}

	for _, workerID := range []string{"alarm_dispatch", "notification_delivery", "youtube_delivery"} {
		state.trackers[workerID] = workercontract.NewExecutorTracker()
		state.totals[workerID] = &workercontract.Counters{}
	}

	return &settings.Config{AlarmWorkerProfile: profile}, state
}

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
		egress.NewIrisMessageSender(stub, egress.WithMarkdownReplies(true), egress.WithRoomChat(staticOpenRooms{})),
		nil,
	)

	require.NoError(t, sender.SendMessageWithClientRequestID(t.Context(), "room-1", "**hello**", "req-1"))

	assert.True(t, stub.markdownSent)
	assert.Equal(t, "room-1", stub.roomID)
	assert.Equal(t, "**hello**", stub.message)
	assert.Equal(t, 1, stub.opts)
}

func TestBuildNotificationEgressRequiresPostgres(t *testing.T) {
	config, state := alarmWorkerTestConfig(t)
	runner, err := buildNotificationEgress(t.Context(), &alarmworker.RuntimeConfig{Config: config}, &sharedmodules.InfraModule{}, nil, state)

	require.Error(t, err)
	assert.Nil(t, runner)
	assert.Contains(t, err.Error(), "postgres is required")
}

func TestBuildAlarmDispatchRunnerBuildsPGRunner(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "true")

	config, state := alarmWorkerTestConfig(t)

	config.AlarmWorkerProfile.AlarmDispatch.MaxBatch = 7

	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	scheduler, err := buildAlarmDispatchRunner(t.Context(), config, infra, egress.NewIrisMessageSender(nil), nil, state)
	require.NoError(t, err)

	runner, ok := scheduler.(*dispatchrun.Runner)
	require.True(t, ok)
	assert.NotNil(t, runner)

	runnerConfig := alarmDispatchRunnerConfig(config)
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
	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "false")

	config, state := alarmWorkerTestConfig(t)

	config.AlarmWorkerProfile.AlarmDispatch.MaxBatch = 9
	config.AlarmWorkerProfile.AlarmDispatch.MaxBatchesPerWake = 3

	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	scheduler, err := buildAlarmDispatchRunner(t.Context(), config, infra, egress.NewIrisMessageSender(nil), nil, state)
	require.NoError(t, err)

	runner, ok := scheduler.(*dispatchrun.Runner)
	require.True(t, ok)
	assert.NotNil(t, runner)

	runnerConfig := alarmDispatchRunnerConfig(config)
	assert.Equal(t, 9, runnerConfig.MaxBatch)
	assert.Equal(t, 3, runnerConfig.MaxBatchesPerWake)
	assert.False(t, runnerConfig.KaringEnabled)
}

func TestBuildEgressDispatchersRespectDisabledFlags(t *testing.T) {
	t.Setenv("YOUTUBE_OUTBOX_V3_HANDOFF_MODE", "off")

	config, state := alarmWorkerTestConfig(t)

	for _, workerID := range []string{"alarm_dispatch", "notification_delivery", "youtube_delivery"} {
		worker := config.AlarmWorkerProfile.Loaded.Profile.Workers[workerID]

		worker.Executor.Enabled = false
		config.AlarmWorkerProfile.Loaded.Profile.Workers[workerID] = worker

		assert.False(t, alarmWorkerExecutorEnabled(config, workerID))
	}

	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	runners, err := buildEgressRunners(t.Context(), &alarmworker.RuntimeConfig{Config: config}, infra, egress.NewIrisMessageSender(nil), nil, state)
	require.NoError(t, err)

	names := make([]string, 0, len(runners))
	for _, runner := range runners {
		names = append(names, runner.Name)
	}

	assert.Equal(t, []string{"alarm-dispatch-maintenance"}, names)
}

func TestBuildEgressRunnersRegistersEveryEnabledWorker(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "false")
	t.Setenv("YOUTUBE_OUTBOX_V3_HANDOFF_MODE", "off")

	config, state := alarmWorkerTestConfig(t)
	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	runners, err := buildEgressRunners(t.Context(), &alarmworker.RuntimeConfig{Config: config}, infra, egress.NewIrisMessageSender(nil), nil, state)
	require.NoError(t, err)

	names := make([]string, 0, len(runners))
	scheduled := make(map[string]bool, len(runners))

	for _, runner := range runners {
		names = append(names, runner.Name)
		scheduled[runner.Name] = runner.Scheduler != nil
	}

	assert.Equal(t, []string{
		"alarm-dispatch",
		"alarm-dispatch-maintenance",
		"youtube-outbox",
		"notification-delivery-outbox",
	}, names)
	assert.True(t, scheduled["alarm-dispatch"])
	assert.True(t, scheduled["youtube-outbox"])
	assert.True(t, scheduled["notification-delivery-outbox"])
}

func TestBuildEgressDispatchersRejectMissingInfraWhenEnabled(t *testing.T) {
	config, state := alarmWorkerTestConfig(t)

	scheduler, err := buildAlarmDispatchRunner(t.Context(), config, nil, egress.NewIrisMessageSender(nil), nil, state)
	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "infra is required")

	scheduler, err = buildDeliveryOutboxDispatcher(config, nil, egress.NewIrisMessageSender(nil), nil, state)
	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "postgres is required")

	scheduler, err = buildYouTubeOutboxDispatcher(config, nil, egress.NewIrisMessageSender(nil), nil, state, handoff.ModeOff)
	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "postgres is required")
}

func TestBuildYouTubeOutboxDispatcherValidatesV3HandoffActivation(t *testing.T) {
	config, state := alarmWorkerTestConfig(t)
	worker := config.AlarmWorkerProfile.Loaded.Profile.Workers["youtube_delivery"]

	worker.Executor.Enabled = false
	config.AlarmWorkerProfile.Loaded.Profile.Workers["youtube_delivery"] = worker

	t.Setenv("YOUTUBE_OUTBOX_V3_HANDOFF_MODE", "shadow")

	_, enabled, err := youtubeOutboxHandoffActivation(config, nil)
	require.Error(t, err)
	assert.False(t, enabled)
	assert.Contains(t, err.Error(), "requires youtube_delivery executor.enabled=true")

	t.Setenv("YOUTUBE_OUTBOX_V3_HANDOFF_MODE", "dual-write")

	_, enabled, err = youtubeOutboxHandoffActivation(config, nil)
	require.Error(t, err)
	assert.False(t, enabled)
	assert.Contains(t, err.Error(), "unsupported mode")

	for _, workerID := range []string{"alarm_dispatch", "notification_delivery"} {
		disabled := config.AlarmWorkerProfile.Loaded.Profile.Workers[workerID]

		disabled.Executor.Enabled = false
		config.AlarmWorkerProfile.Loaded.Profile.Workers[workerID] = disabled
	}

	runners, err := buildEgressRunners(
		t.Context(),
		&alarmworker.RuntimeConfig{Config: config},
		&sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}},
		egress.NewIrisMessageSender(nil),
		nil,
		state,
	)
	require.Error(t, err)
	assert.Nil(t, runners)
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
	config, _ := alarmWorkerTestConfig(t)
	cacheFake := &claimKeyReleaseRecordingCache{}
	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}, Cache: cacheFake}
	consumer := newAlarmDispatchConsumer(config, infra, nil)

	err := consumer.ReleaseClaimKeys(t.Context(), []string{
		"notified:claim:room-1:stream-1:100:live",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, cacheFake.delManyCalls,
		"pg-mode consumer must wire claim-key releaser so DLQ claim keys are released (H14/P1-d)")
	assert.Equal(t, []string{"notified:claim:room-1:stream-1:100:live"}, cacheFake.delManyKeys)
}
