package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
)

type stubStreamProvider struct{}

func (s *stubStreamProvider) GetLiveStreams(context.Context) ([]*domain.Stream, error) { return nil, nil }
func (s *stubStreamProvider) GetUpcomingStreams(context.Context, int) ([]*domain.Stream, error) {
	return nil, nil
}
func (s *stubStreamProvider) GetChannelSchedule(context.Context, string, int, bool) ([]*domain.Stream, error) {
	return nil, nil
}
func (s *stubStreamProvider) GetChannel(context.Context, string) (*domain.Channel, error) { return nil, nil }

func TestContainerClose_CallsCleanupOnce(t *testing.T) {
	t.Parallel()

	calls := 0
	container := &Container{
		cleanup: func() { calls++ },
	}

	container.Close()
	assert.Equal(t, 1, calls)

	var nilContainer *Container
	nilContainer.Close()
}

func TestContainerNewBot_FailsWhenDependenciesMissing(t *testing.T) {
	t.Parallel()

	container := &Container{}
	created, err := container.NewBot()
	require.Error(t, err)
	assert.Nil(t, created)
	assert.Contains(t, err.Error(), "bot dependencies not initialized")
}

func TestContainerGetterMappings(t *testing.T) {
	t.Parallel()

	cacheSvc := &cache.Service{}
	memberRepo := &member.Repository{}
	memberCache := &member.Cache{}
	alarmSvc := testAlarmCRUD{}
	streamSvc := &stubStreamProvider{}
	youtubeSvc := &trackingYouTubeSvc{}
	scheduler := &stubYouTubeScheduler{}
	activityLogger := &activity.Logger{}
	settingsSvc := &stubSettingsReadWriter{}
	aclSvc := &acl.Service{}

	container := &Container{
		botDeps: &bot.Dependencies{
			Cache:      cacheSvc,
			MemberRepo: memberRepo,
			MemberCache: memberCache,
			Alarm:      alarmSvc,
			Holodex:    streamSvc,
			Service:    youtubeSvc,
			Scheduler:  scheduler,
			Activity:   activityLogger,
			Settings:   settingsSvc,
			ACL:        aclSvc,
		},
	}

	assert.Same(t, scheduler, container.GetYouTubeScheduler())
	assert.Same(t, memberRepo, container.GetMemberRepo())
	assert.Same(t, memberCache, container.GetMemberCache())
	assert.Same(t, cacheSvc, container.GetCache())
	assert.Equal(t, alarmSvc, container.GetAlarmService())
	assert.Same(t, streamSvc, container.GetHolodexService())
	assert.Same(t, youtubeSvc, container.GetYouTubeService())
	assert.Same(t, activityLogger, container.GetActivityLogger())
	assert.Same(t, settingsSvc, container.GetSettingsService())
	assert.Same(t, aclSvc, container.GetACLService())
}

func TestContainerGetters_ReturnNilWhenBotDepsMissing(t *testing.T) {
	t.Parallel()

	container := &Container{}
	assert.Nil(t, container.GetYouTubeScheduler())
	assert.Nil(t, container.GetMemberRepo())
	assert.Nil(t, container.GetMemberCache())
	assert.Nil(t, container.GetAlarmService())
	assert.Nil(t, container.GetCache())
	assert.Nil(t, container.GetHolodexService())
	assert.Nil(t, container.GetYouTubeService())
	assert.Nil(t, container.GetActivityLogger())
	assert.Nil(t, container.GetSettingsService())
	assert.Nil(t, container.GetACLService())
}

func TestBuild_FailFastOnNilInputs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	created, err := Build(context.Background(), nil, logger)
	require.Error(t, err)
	assert.Nil(t, created)
	assert.Equal(t, "config must not be nil", err.Error())

	created, err = Build(context.Background(), &config.Config{}, nil)
	require.Error(t, err)
	assert.Nil(t, created)
	assert.Equal(t, "logger must not be nil", err.Error())
}

var _ youtube.Scheduler = (*stubYouTubeScheduler)(nil)
var _ domain.StreamProvider = (*stubStreamProvider)(nil)
var _ settings.ReadWriter = (*stubSettingsReadWriter)(nil)

