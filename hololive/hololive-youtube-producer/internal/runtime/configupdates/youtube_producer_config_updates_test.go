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

package configupdates

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"unsafe"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	svcsettings "github.com/kapu/hololive-shared/pkg/service/settings"
	settingsmocks "github.com/kapu/hololive-shared/pkg/service/settings/mocks"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	sharedlogging "github.com/park285/hololive-bot/shared-go/pkg/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
)

var testLogger = sharedlogging.NewLogger

type fakeYouTubeService struct {
	mu           sync.Mutex
	setCalls     int
	lastEnabled  bool
	proxyEnabled bool
}

func (f *fakeYouTubeService) SetScraperProxyEnabled(enabled bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls++
	f.lastEnabled = enabled
	f.proxyEnabled = enabled
	return true
}

func (f *fakeYouTubeService) ScraperProxyEnabled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.proxyEnabled
}

func (f *fakeYouTubeService) GetChannelStatistics(context.Context, []string) (map[string]*youtube.ChannelStats, error) {
	return map[string]*youtube.ChannelStats{}, nil
}

func (f *fakeYouTubeService) GetRecentVideos(context.Context, string, int64) ([]string, error) {
	return []string{}, nil
}

func TestApplyScraperProxyToggle_NilDeps(t *testing.T) {
	t.Parallel()

	// nil 의존성에서 패닉 없이 실행되어야 함
	assert.NotPanics(t, func() {
		sharedsettings.ApplyScraperProxyToggle(true, nil, nil, nil, testLogger())
	})
}

func TestApplyScraperProxyToggle_EnableDisable(t *testing.T) {
	t.Parallel()

	service := &fakeYouTubeService{}

	sharedsettings.ApplyScraperProxyToggle(true, service, nil, nil, testLogger())
	assert.Equal(t, 1, service.setCalls)
	assert.True(t, service.lastEnabled)

	sharedsettings.ApplyScraperProxyToggle(false, service, nil, nil, testLogger())
	assert.Equal(t, 2, service.setCalls)
	assert.False(t, service.lastEnabled)
}

func TestBuildSubscriber_ApplyScraperProxyPersistsSettingAndUpdatesYouTube(t *testing.T) {
	t.Parallel()

	currentSettings := svcsettings.Settings{
		AlarmAdvanceMinutes: 5,
		ScraperProxyEnabled: false,
		TargetMinutes:       []int{5, 3, 1},
	}
	updateCalls := 0
	settingsService := &settingsmocks.ReadWriter{
		GetFunc: func() svcsettings.Settings {
			return currentSettings
		},
		UpdateFunc: func(newSettings svcsettings.Settings) error {
			updateCalls++
			currentSettings = newSettings
			return nil
		},
	}
	youtubeService := &fakeYouTubeService{}

	applyFn := extractConfigUpdateApplyFn(t, BuildSubscriber(
		testConfigUpdateCacheClient(),
		settingsService,
		nil,
		&providers.YouTubeStack{Service: youtubeService},
		nil,
		testLogger(),
	))

	applyFn(configsub.ConfigUpdate{
		Type:    contractssettings.UpdateTypeScraperProxy,
		Payload: []byte(`{"enabled":true}`),
	})

	require.Equal(t, 1, updateCalls)
	assert.True(t, currentSettings.ScraperProxyEnabled)
	assert.Equal(t, []int{5, 3, 1}, currentSettings.TargetMinutes)
	assert.True(t, youtubeService.ScraperProxyEnabled())
}

func TestBuildSubscriber_IgnoresAlarmAdvanceMinutesAndInvalidPayload(t *testing.T) {
	t.Parallel()

	currentSettings := svcsettings.Settings{
		AlarmAdvanceMinutes: 5,
		ScraperProxyEnabled: false,
	}
	updateCalls := 0
	settingsService := &settingsmocks.ReadWriter{
		GetFunc: func() svcsettings.Settings {
			return currentSettings
		},
		UpdateFunc: func(newSettings svcsettings.Settings) error {
			updateCalls++
			currentSettings = newSettings
			return nil
		},
	}

	applyFn := extractConfigUpdateApplyFn(t, BuildSubscriber(
		testConfigUpdateCacheClient(),
		settingsService,
		nil,
		nil,
		nil,
		testLogger(),
	))

	applyFn(configsub.ConfigUpdate{
		Type:    contractssettings.UpdateTypeScraperProxy,
		Payload: []byte(`{"enabled":`),
	})
	applyFn(configsub.ConfigUpdate{
		Type:    contractssettings.UpdateTypeAlarmAdvanceMinutes,
		Payload: []byte(`{"minutes":30}`),
	})
	applyFn(configsub.ConfigUpdate{
		Type:    "unknown_update_type",
		Payload: []byte(`{}`),
	})

	assert.Equal(t, 0, updateCalls)
	assert.False(t, currentSettings.ScraperProxyEnabled)
}

func TestBuildSubscriber_ApplyScraperProxyLogsPersistError(t *testing.T) {
	t.Parallel()

	currentSettings := svcsettings.Settings{
		AlarmAdvanceMinutes: 5,
		ScraperProxyEnabled: false,
	}
	settingsService := &settingsmocks.ReadWriter{
		GetFunc: func() svcsettings.Settings {
			return currentSettings
		},
		UpdateFunc: func(newSettings svcsettings.Settings) error {
			currentSettings = newSettings
			return errors.New("persist failed")
		},
	}

	applyFn := extractConfigUpdateApplyFn(t, BuildSubscriber(
		testConfigUpdateCacheClient(),
		settingsService,
		nil,
		nil,
		nil,
		testLogger(),
	))

	require.NotPanics(t, func() {
		applyFn(configsub.ConfigUpdate{
			Type:    contractssettings.UpdateTypeScraperProxy,
			Payload: []byte(`{"enabled":true}`),
		})
	})
	assert.True(t, currentSettings.ScraperProxyEnabled)
}

func testConfigUpdateCacheClient() *cachemocks.Client {
	return &cachemocks.Client{
		GetClientFunc: func() valkey.Client { return nil },
	}
}

func extractConfigUpdateApplyFn(t *testing.T, subscriber *configsub.Subscriber) func(configsub.ConfigUpdate) {
	t.Helper()

	require.NotNil(t, subscriber)
	field := reflect.ValueOf(subscriber).Elem().FieldByName("applyFn")
	require.True(t, field.IsValid(), "applyFn field must exist")

	field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	applyFn, ok := field.Interface().(func(configsub.ConfigUpdate))
	require.True(t, ok, "applyFn must be func(configsub.ConfigUpdate)")
	return applyFn
}
