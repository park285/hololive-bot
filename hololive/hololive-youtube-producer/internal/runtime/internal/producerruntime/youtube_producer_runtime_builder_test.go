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
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	settingsmocks "github.com/kapu/hololive-shared/pkg/service/settings/mocks"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/configupdates"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/polling"
	"github.com/park285/shared-go/pkg/runtime/lifecycle"
)

func TestBuildYouTubeProducerHTTPServer_Success(t *testing.T) {
	ingestionConfig := &config.Config{
		Server: config.ServerConfig{
			Port: 30123,
		},
	}

	readiness := newReadinessState(youtubeProducerRuntimeName, ingestionRuntimeFeatures{
		youtubeEnabled:   false,
		photoSyncEnabled: true,
	})
	server, err := buildYouTubeProducerHTTPServer(context.Background(), ingestionConfig, testLogger(), youtubeProducerRuntimeName, readiness)
	require.NoError(t, err)
	require.NotNil(t, server)
	assert.Equal(t, ":30123", server.Addr)
	require.NotNil(t, server.Handler)
}

func TestBuildYouTubeProducerHTTPServer_ReturnsErrorWhenTrustedProxyConfigInvalid(t *testing.T) {
	originalTrustedProxies := constants.ServerConfig.TrustedProxies
	constants.ServerConfig.TrustedProxies = []string{"not-a-valid-proxy-entry"}
	t.Cleanup(func() {
		constants.ServerConfig.TrustedProxies = originalTrustedProxies
	})

	ingestionConfig := &config.Config{
		Server: config.ServerConfig{
			Port: 30123,
		},
	}

	readiness := newReadinessState(youtubeProducerRuntimeName, ingestionRuntimeFeatures{
		youtubeEnabled:   false,
		photoSyncEnabled: true,
	})
	server, err := buildYouTubeProducerHTTPServer(context.Background(), ingestionConfig, testLogger(), youtubeProducerRuntimeName, readiness)
	require.Error(t, err)
	assert.Nil(t, server)
	assert.Contains(t, err.Error(), "build youtube producer router: set trusted proxies")
}

func TestBuildRuntimePhotoSyncService_ReturnsNilWhenDisabled(t *testing.T) {
	ingestionConfig := &config.Config{
		Scraper: config.ScraperConfig{
			ActiveActive: config.ScraperActiveActiveConfig{
				Enabled:    true,
				Namespace:  "test",
				InstanceID: "test-a",
			},
		},
	}
	infra := &youtubeProducerInfrastructure{
		photoSync: &holodex.PhotoSyncService{},
	}

	service := buildRuntimePhotoSyncService(ingestionConfig, ingestionRuntimeFeatures{
		photoSyncEnabled: false,
	}, infra, testLogger())

	assert.Nil(t, service)
}

func TestBuildIngestionRuntimeSpec(t *testing.T) {
	t.Run("youtube producer spec preserves configured flags before big-bang cutover", func(t *testing.T) {
		ingestionConfig := &config.Config{
			Ingestion: config.IngestionConfig{
				YouTubeEnabled:                true,
				PhotoSyncEnabled:              true,
				CommunityShortsBigBangEnabled: false,
			},
		}

		spec := youtubeProducerSpec(ingestionConfig)
		assert.Equal(t, youtubeProducerRuntimeName, spec.name)
		assert.Equal(t, spec.requestedFeatures, spec.features)
		assert.True(t, spec.features.youtubeEnabled)
		assert.True(t, spec.features.photoSyncEnabled)
		assert.False(t, spec.features.communityShortsBigBangEnabled)
	})

	t.Run("youtube producer spec preserves youtube routing during big-bang cutover", func(t *testing.T) {
		ingestionConfig := &config.Config{
			Ingestion: config.IngestionConfig{
				YouTubeEnabled:                true,
				PhotoSyncEnabled:              true,
				CommunityShortsBigBangEnabled: true,
			},
		}

		spec := youtubeProducerSpec(ingestionConfig)
		assert.Equal(t, youtubeProducerRuntimeName, spec.name)
		assert.True(t, spec.requestedFeatures.youtubeEnabled)
		assert.True(t, spec.requestedFeatures.communityShortsBigBangEnabled)
		assert.True(t, spec.features.youtubeEnabled)
		assert.True(t, spec.features.photoSyncEnabled)
		assert.True(t, spec.features.communityShortsBigBangEnabled)
	})

	t.Run("youtube producer spec preserves photo sync request before big-bang cutover", func(t *testing.T) {
		ingestionConfig := &config.Config{
			Ingestion: config.IngestionConfig{
				YouTubeEnabled:                true,
				PhotoSyncEnabled:              true,
				CommunityShortsBigBangEnabled: false,
			},
		}

		spec := youtubeProducerSpec(ingestionConfig)
		assert.Equal(t, youtubeProducerRuntimeName, spec.name)
		assert.True(t, spec.features.youtubeEnabled)
		assert.True(t, spec.features.photoSyncEnabled)
		assert.False(t, spec.features.communityShortsBigBangEnabled)
	})

	t.Run("youtube producer spec does not force youtube on from big-bang flag alone", func(t *testing.T) {
		ingestionConfig := &config.Config{
			Ingestion: config.IngestionConfig{
				YouTubeEnabled:                false,
				PhotoSyncEnabled:              true,
				CommunityShortsBigBangEnabled: true,
			},
		}

		spec := youtubeProducerSpec(ingestionConfig)
		assert.Equal(t, youtubeProducerRuntimeName, spec.name)
		assert.False(t, spec.requestedFeatures.youtubeEnabled)
		assert.True(t, spec.requestedFeatures.photoSyncEnabled)
		assert.True(t, spec.requestedFeatures.communityShortsBigBangEnabled)
		assert.False(t, spec.features.youtubeEnabled)
		assert.True(t, spec.features.photoSyncEnabled)
		assert.True(t, spec.features.communityShortsBigBangEnabled)
	})
}

func TestIngestionRuntimeSpecs_YouTubeProducerOwnsConfiguredYouTubeState(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		ingestionConfig      config.IngestionConfig
		wantYouTube          bool
		wantCommunityBigBang bool
	}{
		"youtube enabled without big-bang still starts producer polling": {
			ingestionConfig: config.IngestionConfig{
				YouTubeEnabled:                true,
				PhotoSyncEnabled:              true,
				CommunityShortsBigBangEnabled: false,
			},
			wantYouTube:          true,
			wantCommunityBigBang: false,
		},
		"big-bang enabled keeps producer polling and observation enabled": {
			ingestionConfig: config.IngestionConfig{
				YouTubeEnabled:                true,
				PhotoSyncEnabled:              true,
				CommunityShortsBigBangEnabled: true,
			},
			wantYouTube:          true,
			wantCommunityBigBang: true,
		},
		"youtube disabled leaves producer polling idle even if photo sync is enabled": {
			ingestionConfig: config.IngestionConfig{
				YouTubeEnabled:                false,
				PhotoSyncEnabled:              true,
				CommunityShortsBigBangEnabled: false,
			},
			wantYouTube:          false,
			wantCommunityBigBang: false,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ingestionConfig := &config.Config{Ingestion: tc.ingestionConfig}
			producerSpec := youtubeProducerSpec(ingestionConfig)

			assert.Equal(t, tc.wantYouTube, producerSpec.features.youtubeEnabled)
			assert.Equal(t, tc.wantCommunityBigBang, producerSpec.features.communityShortsBigBangEnabled)
		})
	}
}

func TestIngestionRuntimeSpecs_AllowYouTubeProducerPhotoSyncOwner(t *testing.T) {
	t.Parallel()

	ingestionConfig := &config.Config{
		Ingestion: config.IngestionConfig{
			YouTubeEnabled:                true,
			PhotoSyncEnabled:              true,
			CommunityShortsBigBangEnabled: true,
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
			ingestionConfig := &config.Config{
				Server: config.ServerConfig{Port: 30123},
				Ingestion: config.IngestionConfig{
					YouTubeEnabled:   true,
					PhotoSyncEnabled: true,
				},
				Scraper: config.ScraperConfig{
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
			youtubeScheduler := &fakeScheduler{}
			memberData := &fakeMemberDataProvider{
				members: []*domain.Member{
					{ChannelID: "active-channel", Name: "active", IsGraduated: false},
					{ChannelID: "graduated-channel", Name: "graduated", IsGraduated: true},
				},
			}

			cleanupCalls := 0
			infra := &youtubeProducerInfrastructure{
				cacheService:    cacheService,
				postgresService: &databasemocks.Client{},
				settingsService: settingsService,
				holodexService:  &holodex.Service{},
				ytStack: &providers.YouTubeStack{
					Service:   youtubeService,
					Scheduler: youtubeScheduler,
				},
				photoSync: &holodex.PhotoSyncService{},
				cleanup:   func() { cleanupCalls++ },
			}

			operationalChannels := mustResolveCommunityShortsOperationalChannels(t, memberData)

			scraperScheduler, registrations, err := polling.BuildComponents(
				ingestionConfig.Scraper,
				infra.postgresService,
				communityshorts.EnabledChannelIDs(operationalChannels),
				communityshorts.EnabledChannelIDs(operationalChannels),
				polling.BuildSharedClient(ingestionConfig.Scraper, infra.cacheService, infra.sharedRL),
				nil,
				nil,
				nil,
				testLogger(),
			)
			require.NoError(t, err)
			require.NotNil(t, scraperScheduler)
			require.Len(t, registrations, 5)
			assert.Equal(t, 5, schedulerJobCount(t, scraperScheduler))

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
			applyFn := extractSubscriberApplyFn(t, configSubscriber)
			applyFn(configsub.ConfigUpdate{
				Type:    contractssettings.UpdateTypeScraperProxy,
				Payload: updatePayload,
			})

			assert.Equal(t, 1, updateCalls)
			assert.Equal(t, tc.updatedProxyEnabled, currentSettings.ScraperProxyEnabled)
			assert.Equal(t, tc.updatedProxyEnabled, youtubeService.ScraperProxyEnabled())

			readiness := newReadinessState(youtubeProducerRuntimeName, ingestionRuntimeFeatures{
				youtubeEnabled:   true,
				photoSyncEnabled: true,
			})
			httpServer, err := buildYouTubeProducerHTTPServer(context.Background(), ingestionConfig, testLogger(), youtubeProducerRuntimeName, readiness)
			require.NoError(t, err)
			require.NotNil(t, httpServer)

			runtime := &YouTubeProducerRuntime{
				Config:           ingestionConfig,
				Logger:           testLogger(),
				Scheduler:        youtubeScheduler,
				ScraperScheduler: scraperScheduler,
				PhotoSync:        infra.photoSync,
				ConfigSubscriber: configSubscriber,
				ServerAddr:       fmt.Sprintf(":%d", ingestionConfig.Server.Port),
				HTTPServer:       httpServer,
				Readiness:        readiness,
				Managed:          lifecycle.NewManaged(infra.cleanup),
			}

			require.NotNil(t, runtime)
			assert.Equal(t, ":30123", runtime.ServerAddr)
			assert.NotNil(t, runtime.Scheduler)
			assert.NotNil(t, runtime.ScraperScheduler)
			assert.NotNil(t, runtime.ConfigSubscriber)
			assert.NotNil(t, runtime.PhotoSync)
			assert.NotNil(t, runtime.HTTPServer)
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
	cacheService := &cachemocks.Client{
		GetClientFunc: func() valkey.Client { return nil },
	}
	youtubeService := &fakeYouTubeService{}

	subscriber := configupdates.BuildSubscriber(
		cacheService,
		settingsService,
		nil,
		&providers.YouTubeStack{Service: youtubeService},
		nil,
		testLogger(),
	)
	applyFn := extractSubscriberApplyFn(t, subscriber)
	applyFn(configsub.ConfigUpdate{
		Type:    contractssettings.UpdateTypeScraperProxy,
		Payload: []byte(`{"enabled":true}`),
	})

	assert.Equal(t, 1, updateCalls)
	assert.True(t, youtubeService.ScraperProxyEnabled())
	assert.False(t, currentSettings.ScraperProxyEnabled)
}
