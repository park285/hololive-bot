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

package botruntime

import (
	"context"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/activity"
)

type stubStreamProvider struct{}

func (s *stubStreamProvider) GetLiveStreams(context.Context) ([]*domain.Stream, error) {
	return nil, nil
}
func (s *stubStreamProvider) GetUpcomingStreams(context.Context, int) ([]*domain.Stream, error) {
	return nil, nil
}

func (s *stubStreamProvider) GetChannelSchedule(context.Context, string, int, bool) ([]*domain.Stream, error) {
	return nil, nil
}
func (s *stubStreamProvider) GetChannel(context.Context, string) (*domain.Channel, error) {
	return nil, nil
}

func TestContainerClose_CallsCleanupOnce(t *testing.T) {
	t.Parallel()

	calls := 0
	container := &Container{
		Managed: lifecycle.NewManaged(func() { calls++ }),
	}

	container.Close()
	assert.Equal(t, 1, calls)
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

	cache := &cache.Service{}
	memberRepo := &member.Repository{}
	memberCache := &member.Cache{}
	alarmSvc := testAlarmCRUD{}
	streamSvc := &stubStreamProvider{}
	youtubeSvc := &trackingYouTubeSvc{}
	activityLogger := &activity.Logger{}
	settingsSvc := &stubSettingsReadWriter{}
	aclSvc := &acl.Service{}

	container := &Container{
		botDeps: &bot.Dependencies{
			Cache:       cache,
			MemberRepo:  memberRepo,
			MemberCache: memberCache,
			Alarm:       alarmSvc,
			Holodex:     streamSvc,
			Service:     youtubeSvc,
			Activity:    activityLogger,
			Settings:    settingsSvc,
			ACL:         aclSvc,
		},
	}

	assert.Same(t, memberRepo, container.GetMemberRepo())
	assert.Same(t, memberCache, container.GetMemberCache())
	assert.Same(t, cache, container.GetCache())
	assert.Equal(t, alarmSvc, container.GetAlarmService())
	assert.Same(t, streamSvc, container.GetHolodexService())
	assert.Same(t, youtubeSvc, container.GetYouTubeService())
	assert.Same(t, activityLogger, container.GetActivityLogger())
	assert.Same(t, settingsSvc, container.GetSettingsService())
	assert.Same(t, aclSvc, container.GetACLService())
}

func TestBuild_FailFastOnNilInputs(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)

	created, err := Build(t.Context(), nil, logger)
	require.Error(t, err)
	assert.Nil(t, created)
	assert.Equal(t, "config must not be nil", err.Error())

	created, err = Build(t.Context(), &config.Config{}, nil)
	require.Error(t, err)
	assert.Nil(t, created)
	assert.Equal(t, "logger must not be nil", err.Error())
}

var (
	_ domain.StreamProvider = (*stubStreamProvider)(nil)
	_ settings.ReadWriter   = (*stubSettingsReadWriter)(nil)
)
