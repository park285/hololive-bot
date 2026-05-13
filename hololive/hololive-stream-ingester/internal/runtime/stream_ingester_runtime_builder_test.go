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

package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"

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
	communityshorts "github.com/kapu/hololive-stream-ingester/internal/communityshorts"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"
)

func TestBuildStreamIngesterHTTPServer_Success(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port: 30123,
		},
	}

	readiness := newIngestionReadinessState(streamIngesterRuntimeName, ingestionRuntimeFeatures{
		youtubeEnabled:   false,
		photoSyncEnabled: true,
	})
	server, err := buildStreamIngesterHTTPServer(context.Background(), cfg, testLogger(), streamIngesterRuntimeName, readiness)
	require.NoError(t, err)
	require.NotNil(t, server)
	assert.Equal(t, ":30123", server.Addr)
	require.NotNil(t, server.Handler)
}

func TestBuildStreamIngesterHTTPServer_ReturnsErrorWhenTrustedProxyConfigInvalid(t *testing.T) {
	originalTrustedProxies := constants.ServerConfig.TrustedProxies
	constants.ServerConfig.TrustedProxies = []string{"not-a-valid-proxy-entry"}
	t.Cleanup(func() {
		constants.ServerConfig.TrustedProxies = originalTrustedProxies
	})

	cfg := &config.Config{
		Server: config.ServerConfig{
			Port: 30123,
		},
	}

	readiness := newIngestionReadinessState(streamIngesterRuntimeName, ingestionRuntimeFeatures{
		youtubeEnabled:   false,
		photoSyncEnabled: true,
	})
	server, err := buildStreamIngesterHTTPServer(context.Background(), cfg, testLogger(), streamIngesterRuntimeName, readiness)
	require.Error(t, err)
	assert.Nil(t, server)
	assert.Contains(t, err.Error(), "build stream ingester router: set trusted proxies")
}

func TestBuildIngestionRuntimeSpec(t *testing.T) {
	t.Run("stream ingester spec preserves configured flags before big-bang cutover", func(t *testing.T) {
		cfg := &config.Config{
			Ingestion: config.IngestionConfig{
				YouTubeEnabled:                true,
				PhotoSyncEnabled:              true,
				CommunityShortsBigBangEnabled: false,
			},
		}

		spec := streamIngesterSpec(cfg)
		assert.Equal(t, streamIngesterRuntimeName, spec.name)
		assert.Equal(t, spec.requestedFeatures, spec.features)
		assert.True(t, spec.features.youtubeEnabled)
		assert.True(t, spec.features.photoSyncEnabled)
		assert.False(t, spec.features.communityShortsBigBangEnabled)
	})

	t.Run("stream ingester spec disables youtube routing after big-bang cutover", func(t *testing.T) {
		cfg := &config.Config{
			Ingestion: config.IngestionConfig{
				YouTubeEnabled:                true,
				PhotoSyncEnabled:              true,
				CommunityShortsBigBangEnabled: true,
			},
		}

		spec := streamIngesterSpec(cfg)
		assert.Equal(t, streamIngesterRuntimeName, spec.name)
		assert.True(t, spec.requestedFeatures.youtubeEnabled)
		assert.True(t, spec.requestedFeatures.communityShortsBigBangEnabled)
		assert.False(t, spec.features.youtubeEnabled)
		assert.True(t, spec.features.photoSyncEnabled)
		assert.False(t, spec.features.communityShortsBigBangEnabled)
	})

	t.Run("youtube scraper spec stays idle until big-bang cutover", func(t *testing.T) {
		cfg := &config.Config{
			Ingestion: config.IngestionConfig{
				YouTubeEnabled:                true,
				PhotoSyncEnabled:              true,
				CommunityShortsBigBangEnabled: false,
			},
		}

		spec := youtubeScraperSpec(cfg)
		assert.Equal(t, youtubeScraperRuntimeName, spec.name)
		assert.False(t, spec.features.youtubeEnabled)
		assert.False(t, spec.features.photoSyncEnabled)
		assert.False(t, spec.features.communityShortsBigBangEnabled)
	})

	t.Run("youtube scraper spec becomes exclusive youtube runtime after big-bang cutover", func(t *testing.T) {
		cfg := &config.Config{
			Ingestion: config.IngestionConfig{
				YouTubeEnabled:                false,
				PhotoSyncEnabled:              true,
				CommunityShortsBigBangEnabled: true,
			},
		}

		spec := youtubeScraperSpec(cfg)
		assert.Equal(t, youtubeScraperRuntimeName, spec.name)
		assert.False(t, spec.requestedFeatures.youtubeEnabled)
		assert.True(t, spec.requestedFeatures.communityShortsBigBangEnabled)
		assert.True(t, spec.features.youtubeEnabled)
		assert.False(t, spec.features.photoSyncEnabled)
		assert.True(t, spec.features.communityShortsBigBangEnabled)
	})
}

func TestIngestionRuntimeSpecs_KeepYouTubeOwnershipExclusiveAcrossRolloutStates(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		cfg                config.IngestionConfig
		wantStreamYouTube  bool
		wantScraperYouTube bool
		wantActiveOwners   int
	}{
		"legacy rollout keeps stream-ingester as the only youtube owner": {
			cfg: config.IngestionConfig{
				YouTubeEnabled:                true,
				PhotoSyncEnabled:              true,
				CommunityShortsBigBangEnabled: false,
			},
			wantStreamYouTube:  true,
			wantScraperYouTube: false,
			wantActiveOwners:   1,
		},
		"big-bang rollout moves youtube ownership to youtube-scraper": {
			cfg: config.IngestionConfig{
				YouTubeEnabled:                true,
				PhotoSyncEnabled:              true,
				CommunityShortsBigBangEnabled: true,
			},
			wantStreamYouTube:  false,
			wantScraperYouTube: true,
			wantActiveOwners:   1,
		},
		"big-bang rollout still activates youtube-scraper when legacy youtube is off": {
			cfg: config.IngestionConfig{
				YouTubeEnabled:                false,
				PhotoSyncEnabled:              true,
				CommunityShortsBigBangEnabled: true,
			},
			wantStreamYouTube:  false,
			wantScraperYouTube: true,
			wantActiveOwners:   1,
		},
		"youtube disabled everywhere leaves both runtimes idle": {
			cfg: config.IngestionConfig{
				YouTubeEnabled:                false,
				PhotoSyncEnabled:              true,
				CommunityShortsBigBangEnabled: false,
			},
			wantStreamYouTube:  false,
			wantScraperYouTube: false,
			wantActiveOwners:   0,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{Ingestion: tc.cfg}
			streamSpec := streamIngesterSpec(cfg)
			scraperSpec := youtubeScraperSpec(cfg)

			assert.Equal(t, tc.wantStreamYouTube, streamSpec.features.youtubeEnabled)
			assert.Equal(t, tc.wantScraperYouTube, scraperSpec.features.youtubeEnabled)
			assert.Equal(t, tc.wantScraperYouTube, scraperSpec.features.communityShortsBigBangEnabled)
			assert.False(t, streamSpec.features.communityShortsBigBangEnabled)

			activeOwners := 0
			if streamSpec.features.youtubeEnabled {
				activeOwners++
			}
			if scraperSpec.features.youtubeEnabled {
				activeOwners++
			}
			assert.Equal(t, tc.wantActiveOwners, activeOwners)
		})
	}
}

func TestBuildStreamIngesterRuntime_NormalBuildWithAllDependencies(t *testing.T) {
	tests := map[string]struct {
		initialProxyEnabled bool
		updatedProxyEnabled bool
	}{
		"proxy enabled -> disabled": {initialProxyEnabled: true, updatedProxyEnabled: false},
		"proxy disabled -> enabled": {initialProxyEnabled: false, updatedProxyEnabled: true},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{
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
			infra := &streamIngesterInfrastructure{
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

			scraperScheduler, outboxDispatcher, registrations, err := buildStreamIngesterYouTubeComponents(
				cfg.Scraper,
				infra.postgresService,
				communityshorts.EnabledChannelIDs(operationalChannels),
				communityshorts.EnabledChannelIDs(operationalChannels),
				buildSharedYouTubeScraperClient(cfg.Scraper, infra.cacheService, infra.sharedRL),
				infra.cacheService,
				infra.irisClient,
				infra.templateRenderer,
				nil,
				nil,
				testLogger(),
			)
			require.NoError(t, err)
			require.NotNil(t, scraperScheduler)
			require.NotNil(t, outboxDispatcher)
			require.Len(t, registrations, 5)
			assert.Equal(t, 5, schedulerJobCount(t, scraperScheduler))

			configSubscriber := buildStreamIngesterConfigSubscriber(
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

			readiness := newIngestionReadinessState(streamIngesterRuntimeName, ingestionRuntimeFeatures{
				youtubeEnabled:   true,
				photoSyncEnabled: true,
			})
			httpServer, err := buildStreamIngesterHTTPServer(context.Background(), cfg, testLogger(), streamIngesterRuntimeName, readiness)
			require.NoError(t, err)
			require.NotNil(t, httpServer)

			runtime := &StreamIngesterRuntime{
				Config:           cfg,
				Logger:           testLogger(),
				Scheduler:        youtubeScheduler,
				ScraperScheduler: scraperScheduler,
				PhotoSync:        infra.photoSync,
				OutboxDispatcher: outboxDispatcher,
				ConfigSubscriber: configSubscriber,
				ServerAddr:       fmt.Sprintf(":%d", cfg.Server.Port),
				HttpServer:       httpServer,
				Readiness:        readiness,
				Managed:          lifecycle.NewManaged(infra.cleanup),
			}

			require.NotNil(t, runtime)
			assert.Equal(t, ":30123", runtime.ServerAddr)
			assert.NotNil(t, runtime.Scheduler)
			assert.NotNil(t, runtime.ScraperScheduler)
			assert.NotNil(t, runtime.OutboxDispatcher)
			assert.NotNil(t, runtime.ConfigSubscriber)
			assert.NotNil(t, runtime.PhotoSync)
			assert.NotNil(t, runtime.HttpServer)
			assert.Equal(t, 0, cleanupCalls)

			runtime.Close()
			assert.Equal(t, 1, cleanupCalls)
		})
	}
}

func TestBuildStreamIngesterConfigSubscriber_ScraperProxyUpdateFailure(t *testing.T) {
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

	subscriber := buildStreamIngesterConfigSubscriber(
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
