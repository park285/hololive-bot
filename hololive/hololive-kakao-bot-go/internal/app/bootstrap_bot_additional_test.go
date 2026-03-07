package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
	"github.com/kapu/hololive-shared/pkg/config"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
)

func TestProvideBot_ErrorWrapped(t *testing.T) {
	t.Parallel()

	created, err := ProvideBot(nil)
	require.Error(t, err)
	assert.Nil(t, created)
	assert.Contains(t, err.Error(), "failed to create bot")
}

func TestProvideTriggerHandler_ReturnsUsableHandler(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := ProvideTriggerHandler(nil, nil, nil, logger)
	require.NotNil(t, handler)

	router := gin.New()
	handler.RegisterInternalRoutesWithAuth(router.Group(""), "")

	req := httptest.NewRequest(http.MethodPost, triggercontracts.MajorEventWeeklyPath, nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(t, http.StatusServiceUnavailable, res.Code)
}

func TestBuildBotWebhookHandler_ConstructsAndHandlesMethodGuard(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Iris: config.IrisConfig{
			WebhookToken: "test-token",
		},
		Webhook: config.WebhookConfig{
			WorkerCount:    1,
			QueueSize:      8,
			EnqueueTimeout: 10 * time.Millisecond,
			HandlerTimeout: 50 * time.Millisecond,
		},
	}
	deps := botWebhookRuntimeDependencies{
		cache: &cachemocks.Client{
			GetClientFunc: func() valkey.Client { return nil },
		},
	}

	handler := buildBotWebhookHandler(cfg, nil, deps, nil)
	require.NotNil(t, handler)
	t.Cleanup(func() {
		_ = handler.Close()
	})

	router := gin.New()
	router.Any("/webhook/iris", handler.Handle)

	req := httptest.NewRequest(http.MethodGet, "/webhook/iris", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	assert.Equal(t, http.StatusMethodNotAllowed, res.Code)
}

func TestBuildBotRuntime_FailsFastWhenBotProvisionFails(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runtime, err := buildBotRuntime(context.Background(), nil, logger, nil)
	require.Error(t, err)
	assert.Nil(t, runtime)
	assert.Contains(t, err.Error(), "failed to create bot")
}

func TestBuildAlarmRuntimeScheduler_ConstructsScheduler(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Notification: config.NotificationConfig{
			AdvanceMinutes: []int{5, 3, 1},
		},
	}
	infra := &coreInfrastructure{
		deps: &bot.Dependencies{
			Cache:  &cachemocks.Client{},
			Chzzk:  &chzzk.Client{},
			Twitch: &twitch.Client{},
		},
		holodexService: &holodex.Service{},
		alarmService:   &notification.AlarmService{},
	}

	scheduler, err := buildAlarmRuntimeScheduler(cfg, infra, logger)
	require.NoError(t, err)
	assert.NotNil(t, scheduler)
}
