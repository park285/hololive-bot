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

package producerruntime

import (
	"errors"
	"fmt"
	"testing"
	"time"

	configsettings "github.com/kapu/hololive-shared/pkg/config/settings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	dbtest "github.com/kapu/hololive-dbtest"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"

	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	settingsmocks "github.com/kapu/hololive-shared/pkg/service/settings/mocks"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/configupdates"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polling"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
)

func TestBuildRuntimePhotoSyncService_ReturnsNilWhenDisabled(t *testing.T) {
	ingestionConfig := &configsettings.Config{
		Scraper: configsettings.ScraperConfig{
			ActiveActive: configsettings.ScraperActiveActiveConfig{
				Enabled:    true,
				Namespace:  "test",
				InstanceID: "test-a",
			},
		},
	}
	infra := &youtubeProducerInfrastructure{
		photoSync: &holodexprovider.PhotoSyncService{},
	}

	service := buildRuntimePhotoSyncService(ingestionConfig, ingestionRuntimeFeatures{
		photoSyncEnabled: false,
	}, infra, testLogger())

	assert.Nil(t, service)
}

func TestBuildIngestionRuntimeSpec(t *testing.T) {
	t.Run("youtube producer spec preserves configured flags", func(t *testing.T) {
		ingestionConfig := &configsettings.Config{
			Ingestion: configsettings.IngestionConfig{
				YouTubeEnabled:   true,
				PhotoSyncEnabled: true,
			},
		}

		spec := youtubeProducerSpec(ingestionConfig)
		assert.Equal(t, youtubeProducerRuntimeName, spec.name)
		assert.True(t, spec.features.youtubeEnabled)
		assert.True(t, spec.features.photoSyncEnabled)
	})

	t.Run("youtube producer spec preserves photo sync request", func(t *testing.T) {
		ingestionConfig := &configsettings.Config{
			Ingestion: configsettings.IngestionConfig{
				YouTubeEnabled:   true,
				PhotoSyncEnabled: true,
			},
		}

		spec := youtubeProducerSpec(ingestionConfig)
		assert.Equal(t, youtubeProducerRuntimeName, spec.name)
		assert.True(t, spec.features.youtubeEnabled)
		assert.True(t, spec.features.photoSyncEnabled)
	})

	t.Run("youtube producer spec keeps youtube off when disabled", func(t *testing.T) {
		ingestionConfig := &configsettings.Config{
			Ingestion: configsettings.IngestionConfig{
				YouTubeEnabled:   false,
				PhotoSyncEnabled: true,
			},
		}

		spec := youtubeProducerSpec(ingestionConfig)
		assert.Equal(t, youtubeProducerRuntimeName, spec.name)
		assert.False(t, spec.features.youtubeEnabled)
		assert.True(t, spec.features.photoSyncEnabled)
		assert.False(t, spec.features.activeActiveEnabled)
	})
}

func TestIngestionRuntimeSpecs_YouTubeProducerOwnsConfiguredYouTubeState(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		ingestionConfig configsettings.IngestionConfig
		wantYouTube     bool
	}{
		"youtube enabled starts producer polling": {
			ingestionConfig: configsettings.IngestionConfig{
				YouTubeEnabled:   true,
				PhotoSyncEnabled: true,
			},
			wantYouTube: true,
		},
		"youtube disabled leaves producer polling idle even if photo sync is enabled": {
			ingestionConfig: configsettings.IngestionConfig{
				YouTubeEnabled:   false,
				PhotoSyncEnabled: true,
			},
			wantYouTube: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ingestionConfig := &configsettings.Config{Ingestion: tc.ingestionConfig}
			producerSpec := youtubeProducerSpec(ingestionConfig)

			assert.Equal(t, tc.wantYouTube, producerSpec.features.youtubeEnabled)
		})
	}
}

func TestIngestionRuntimeSpecs_AllowYouTubeProducerPhotoSyncOwner(t *testing.T) {
	t.Parallel()

	ingestionConfig := &configsettings.Config{
		Ingestion: configsettings.IngestionConfig{
			YouTubeEnabled:   true,
			PhotoSyncEnabled: true,
		},
	}
	producerSpec := youtubeProducerSpec(ingestionConfig)

	activePhotoSyncOwners := 0
	youtubeProducerActive := true
	if youtubeProducerActive && producerSpec.features.photoSyncEnabled {
		activePhotoSyncOwners++
	}

	assert.True(t, producerSpec.features.photoSyncEnabled)
	assert.True(t, producerSpec.features.youtubeEnabled)
	assert.Equal(t, 1, activePhotoSyncOwners)
}

func TestActiveActiveInitialJitterIsDeterministicAndBounded(t *testing.T) {
	first := activeActiveInitialJitter("youtube-producer-a")
	second := activeActiveInitialJitter("youtube-producer-a")
	other := activeActiveInitialJitter("youtube-producer-b")

	require.Equal(t, first, second)
	require.GreaterOrEqual(t, first, time.Duration(0))
	require.Less(t, first, activeActivePollTargetRefreshMaxJitter)
	require.NotEqual(t, first, other)
	require.Equal(t, time.Duration(0), activeActiveInitialJitter(" "))
}

func TestBuildYouTubeProducerRuntime_NormalBuildWithAllDependencies(t *testing.T) {
	tests := map[string]struct {
		initialProxyEnabled bool
		updatedProxyEnabled bool
	}{
		"proxy enabled -> disabled": {initialProxyEnabled: true, updatedProxyEnabled: false},
		"proxy disabled -> enabled": {initialProxyEnabled: false, updatedProxyEnabled: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			ingestionConfig := &configsettings.Config{
				Server: configsettings.ServerConfig{Port: 30123},
				Ingestion: configsettings.IngestionConfig{
					YouTubeEnabled:   true,
					PhotoSyncEnabled: true,
				},
				Scraper: configsettings.ScraperConfig{
					ProxyEnabled: true,
					ProxyURL:     "socks5://proxy.internal:1080",
				},
			}

			cacheService := &cachemocks.Client{
				GetClientFunc: func() valkey.Client { return nil },
			}

			currentSettings := settings.Settings{
				AlarmAdvanceMinutes: 5,
				ScraperProxyEnabled: tc.initialProxyEnabled,
			}
			updateCalls := 0
			settingsService := &settingsmocks.ReadWriter{
				GetFunc: func() settings.Settings {
					return currentSettings
				},
				UpdateFunc: func(newSettings settings.Settings) error {
					updateCalls++
					currentSettings = newSettings
					return nil
				},
			}

			youtubeService := &fakeYouTubeService{}
			memberData := &fakeMemberDataProvider{
				members: []*domain.Member{
					{ChannelID: "active-channel", Name: "active", IsGraduated: false},
					{ChannelID: "graduated-channel", Name: "graduated", IsGraduated: true},
				},
			}

			cleanupCalls := 0
			pool := dbtest.NewPool(t)
			infra := &youtubeProducerInfrastructure{
				cacheService:    cacheService,
				postgresService: &databasemocks.Client{GetPoolFunc: func() *pgxpool.Pool { return pool }},
				settingsService: settingsService,
				holodexService:  &holodexprovider.Service{},
				ytStack: &providers.YouTubeStack{
					Service: youtubeService,
				},
				photoSync: &holodexprovider.PhotoSyncService{},
				cleanup:   func() { cleanupCalls++ },
			}

			operationalChannels := mustResolveCommunityShortsOperationalChannels(t, memberData)

			scraperScheduler, registrations, err := polling.BuildComponents(
				&ingestionConfig.Scraper,
				infra.postgresService,
				communityshorts.EnabledChannelIDs(operationalChannels),
				communityshorts.EnabledChannelIDs(operationalChannels),
				polling.BuildSharedClient(&ingestionConfig.Scraper, infra.cacheService, infra.sharedRL),
				nil,
				testLogger(),
			)
			require.NoError(t, err)
			require.NotNil(t, scraperScheduler)
			require.Len(t, registrations, 1)
			assert.Equal(t, 1, schedulerJobCount(t, scraperScheduler))

			configSubscriber := configupdates.BuildSubscriber(
				infra.cacheService,
				infra.settingsService,
				infra.holodexService,
				infra.ytStack,
				scraperScheduler,
				testLogger(),
			)
			require.NotNil(t, configSubscriber)

			desiredProxyState := infra.settingsService.Get().ScraperProxyEnabled
			sharedsettings.ApplyScraperProxyToggle(
				desiredProxyState,
				infra.ytStack.GetService(),
				infra.holodexService,
				scraperScheduler,
				testLogger(),
			)
			assert.Equal(t, tc.initialProxyEnabled, youtubeService.ScraperProxyEnabled())

			updatePayload := []byte(`{"enabled":false}`)
			if tc.updatedProxyEnabled {
				updatePayload = []byte(`{"enabled":true}`)
			}
			applyFn := newTestYouTubeProducerConfigApplyFn(
				t,
				settingsService,
				&providers.YouTubeStack{Service: youtubeService},
				scraperScheduler,
				testLogger(),
			)
			applyFn(contractssettings.ConfigUpdateV1{
				Type:    contractssettings.UpdateTypeScraperProxy,
				Payload: updatePayload,
			})

			assert.Equal(t, 1, updateCalls)
			assert.Equal(t, tc.updatedProxyEnabled, currentSettings.ScraperProxyEnabled)
			assert.Equal(t, tc.updatedProxyEnabled, youtubeService.ScraperProxyEnabled())

			readiness := newReadinessState(ingestionRuntimeFeatures{
				youtubeEnabled:   true,
				photoSyncEnabled: true,
			})

			runtime := &YouTubeProducerRuntime{
				Config:           ingestionConfig,
				Logger:           testLogger(),
				ScraperScheduler: scraperScheduler,
				PhotoSync:        infra.photoSync,
				ConfigSubscriber: configSubscriber,
				ServerAddr:       fmt.Sprintf(":%d", ingestionConfig.Server.Port),
				Readiness:        readiness,
				Managed:          lifecycle.NewManaged(infra.cleanup),
			}

			require.NotNil(t, runtime)
			assert.Equal(t, ":30123", runtime.ServerAddr)
			assert.NotNil(t, runtime.ScraperScheduler)
			assert.NotNil(t, runtime.ConfigSubscriber)
			assert.NotNil(t, runtime.PhotoSync)
			assert.Equal(t, 0, cleanupCalls)

			runtime.Close()
			assert.Equal(t, 1, cleanupCalls)
		})
	}
}

func TestBuildYouTubeProducerConfigSubscriber_ScraperProxyUpdateFailure(t *testing.T) {
	currentSettings := settings.Settings{
		AlarmAdvanceMinutes: 5,
		ScraperProxyEnabled: false,
	}
	updateCalls := 0

	settingsService := &settingsmocks.ReadWriter{
		GetFunc: func() settings.Settings {
			return currentSettings
		},
		UpdateFunc: func(settings.Settings) error {
			updateCalls++
			return errors.New("write failed")
		},
	}
	youtubeService := &fakeYouTubeService{}

	applyFn := newTestYouTubeProducerConfigApplyFn(
		t,
		settingsService,
		&providers.YouTubeStack{Service: youtubeService},
		nil,
		testLogger(),
	)
	applyFn(contractssettings.ConfigUpdateV1{
		Type:    contractssettings.UpdateTypeScraperProxy,
		Payload: []byte(`{"enabled":true}`),
	})

	assert.Equal(t, 1, updateCalls)
	assert.True(t, youtubeService.ScraperProxyEnabled())
	assert.False(t, currentSettings.ScraperProxyEnabled)
}
