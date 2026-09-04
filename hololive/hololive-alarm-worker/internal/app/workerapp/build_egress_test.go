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

// Runtime client와 alarm-worker sender 사이의 typed compile-time 계약을 고정한다.
var _ egress.IrisClient = (*delivery.RuntimeIrisClient)(nil)

const workerappTestOpenRoom = "open"

type youtubeOutboxKaringCapableSender interface {
	RegularChat(ctx context.Context, roomID string) bool
	SendYouTubeOutboxKaring(ctx context.Context, roomID string, payload *domain.YouTubeOutboxDispatchPayload) error
}

type workerappTestRooms map[string]string

func (rooms workerappTestRooms) OpenChat(_ context.Context, roomID string) bool {
	return rooms[roomID] == workerappTestOpenRoom
}

func (rooms workerappTestRooms) RegularChat(_ context.Context, roomID string) bool {
	return rooms[roomID] == "regular"
}

type clientRequestIDRecordingIrisSender struct {
	roomID          string
	message         string
	opts            int
	markdownRoomID  string
	markdownMessage string
	markdownOpts    int
}

func (s *clientRequestIDRecordingIrisSender) SendMessage(_ context.Context, roomID, message string, opts ...iris.SendOption) error {
	s.roomID = roomID
	s.message = message
	s.opts = len(opts)

	return nil
}

func (s *clientRequestIDRecordingIrisSender) SendMarkdown(_ context.Context, roomID, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	s.markdownRoomID = roomID
	s.markdownMessage = message
	s.markdownOpts = len(opts)

	return &iris.ReplyAcceptedResponse{}, nil
}

func (*clientRequestIDRecordingIrisSender) SendKaringContentList(context.Context, iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error) {
	return &iris.KaringDryRunResponse{Success: true, Delivery: "queued", RequestID: "request-1"}, nil
}

func (*clientRequestIDRecordingIrisSender) GetReplyStatus(_ context.Context, requestID string) (*iris.ReplyStatusSnapshot, error) {
	return &iris.ReplyStatusSnapshot{RequestID: requestID, State: "handoff_completed"}, nil
}

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

func TestBuildYouTubeOutboxSenderScopesKaringToRegularChats(t *testing.T) {
	irisSender := egress.NewIrisMessageSender(nil, egress.WithRoomChat(workerappTestRooms{
		"regular":             "regular",
		workerappTestOpenRoom: workerappTestOpenRoom,
	}))

	sender := buildYouTubeOutboxSender(irisSender, nil)

	karing, ok := sender.(youtubeOutboxKaringCapableSender)
	require.True(t, ok)
	assert.True(t, karing.RegularChat(t.Context(), "regular"))
	assert.False(t, karing.RegularChat(t.Context(), workerappTestOpenRoom))
	assert.False(t, karing.RegularChat(t.Context(), "missing"))
}

func TestBuildYouTubeOutboxSenderPreservesOpenChatMarkdownLane(t *testing.T) {
	stub := &clientRequestIDRecordingIrisSender{}
	irisSender := egress.NewIrisMessageSender(
		stub,
		egress.WithMarkdownReplies(true),
		egress.WithRoomChat(workerappTestRooms{workerappTestOpenRoom: workerappTestOpenRoom}),
	)
	sender := buildYouTubeOutboxSender(irisSender, nil)

	require.NoError(t, sender.SendMessage(t.Context(), workerappTestOpenRoom, "**hello**"))

	assert.Equal(t, workerappTestOpenRoom, stub.markdownRoomID)
	assert.Equal(t, "**hello**", stub.markdownMessage)
	assert.Zero(t, stub.markdownOpts)
	assert.Empty(t, stub.roomID)
}

func TestYouTubeOutboxKaringSenderPreservesClientRequestIDOptionThroughEgress(t *testing.T) {
	stub := &clientRequestIDRecordingIrisSender{}
	sender := dispatchrun.NewYouTubeOutboxKaringSender(egress.NewIrisMessageSender(stub), nil)

	require.NoError(t, sender.SendMessageWithClientRequestID(t.Context(), "room-1", "hello", "req-1"))

	assert.Equal(t, "room-1", stub.roomID)
	assert.Equal(t, "hello", stub.message)
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
}

func TestBuildAlarmDispatchRunnerHonorsBatchEnv(t *testing.T) {
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
