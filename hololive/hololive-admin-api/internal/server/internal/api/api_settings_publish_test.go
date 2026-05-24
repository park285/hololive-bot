package api

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	json "github.com/park285/shared-go/pkg/json"
	"github.com/valkey-io/valkey-go"

	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/configsub"
	"github.com/kapu/hololive-shared/pkg/service/settings"
)

func TestSettingsAPIHandler_UpdateSettings_UsesCacheBackedPublisher(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	mini := miniredis.RunT(t)
	subscriber := mini.NewSubscriber()
	subscriber.Subscribe(configsub.DefaultChannel)
	t.Cleanup(subscriber.Close)
	receivedMessages := make(chan miniredis.PubsubMessage, 2)
	go func() {
		for range 2 {
			message, ok := <-subscriber.Messages()
			if !ok {
				return
			}
			receivedMessages <- message
		}
	}()

	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{mini.Addr()},
		DisableCache:      true,
		ForceSingleClient: true,
	})
	if err != nil {
		t.Fatalf("create valkey client: %v", err)
	}
	t.Cleanup(func() {
		client.Close()
	})

	settingsService := settings.NewSettingsService(filepath.Join(t.TempDir(), "settings.json"), settings.Settings{
		AlarmAdvanceMinutes: 5,
		ScraperProxyEnabled: false,
	}, newDiscardLogger())

	handler := &SettingsAPIHandler{Handler: &Handler{
		logger:          newDiscardLogger(),
		activity:        newActivityLoggerForTest(t),
		settings:        settingsService,
		settingsApplier: testSettingsApplier{},
		valkeyCache: &cachemocks.Client{
			GetClientFunc: func() valkey.Client {
				return client
			},
		},
	}}

	ctx, rec := newAPITestContext(http.MethodPatch, "/api/holo/settings", []byte(`{"alarmAdvanceMinutes":7,"scraperProxyEnabled":true}`))
	handler.UpdateSettings(ctx)

	if rec.Code != http.StatusOK {
		t.Fatalf("UpdateSettings status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	runtime, ok := payload["runtime"].(map[string]any)
	if !ok {
		t.Fatalf("runtime payload missing: %#v", payload["runtime"])
	}
	if got := runtime["config_publish_scraper_proxy"]; got != true {
		t.Fatalf("config_publish_scraper_proxy=%v want=true", got)
	}
	if got := runtime["config_publish_alarm_advance_minutes"]; got != true {
		t.Fatalf("config_publish_alarm_advance_minutes=%v want=true", got)
	}

	updates := map[string]configsub.ConfigUpdate{}
	for range 2 {
		select {
		case message := <-receivedMessages:
			var update configsub.ConfigUpdate
			if err := json.Unmarshal([]byte(message.Message), &update); err != nil {
				t.Fatalf("decode published update: %v", err)
			}
			updates[update.Type] = update
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for published config updates; got=%d", len(updates))
		}
	}

	scraperUpdate, ok := updates[contractssettings.UpdateTypeScraperProxy]
	if !ok {
		t.Fatalf("missing scraper proxy update: %+v", updates)
	}
	var scraperPayload contractssettings.ScraperProxyPayloadV1
	if err := json.Unmarshal(scraperUpdate.Payload, &scraperPayload); err != nil {
		t.Fatalf("decode scraper proxy payload: %v", err)
	}
	if !scraperPayload.Enabled {
		t.Fatalf("scraper proxy enabled=%v want=true", scraperPayload.Enabled)
	}

	alarmUpdate, ok := updates[contractssettings.UpdateTypeAlarmAdvanceMinutes]
	if !ok {
		t.Fatalf("missing alarm advance update: %+v", updates)
	}
	var alarmPayload contractssettings.AlarmAdvanceMinutesPayloadV1
	if err := json.Unmarshal(alarmUpdate.Payload, &alarmPayload); err != nil {
		t.Fatalf("decode alarm advance payload: %v", err)
	}
	if alarmPayload.Minutes != 7 {
		t.Fatalf("alarm advance minutes=%d want=7", alarmPayload.Minutes)
	}

	if err := client.Do(context.Background(), client.B().Ping().Build()).Error(); err != nil {
		t.Fatalf("valkey client unhealthy after publish path: %v", err)
	}
}
