package app

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"

	"github.com/park285/shared-go/pkg/runtime/bootstrap"

	"github.com/kapu/hololive-admin-api/internal/server"
)

type memberNewsRunNowStub struct {
	calls int
	err   error
}

func (s *memberNewsRunNowStub) SendMemberNewsWeekly(context.Context) error {
	s.calls++
	return s.err
}

func TestNormalizeRuntimeBuildInputsDefaultsTODOContext(t *testing.T) {
	t.Parallel()

	ctx, err := bootstrap.NormalizeRuntimeBuildInputs(context.TODO(), &config.Config{}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NormalizeRuntimeBuildInputs() error = %v", err)
	}
	if ctx == nil {
		t.Fatal("NormalizeRuntimeBuildInputs() returned nil context")
	}
}

func TestNewAdminAPIRuntimeInitializesServerAndCleanup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cleanupCalls := 0
	logger := slog.New(slog.DiscardHandler)
	runtime := newAdminAPIRuntime(&config.Config{}, logger, "127.0.0.1:0", gin.New(), func() {
		cleanupCalls++
	})

	if runtime == nil {
		t.Fatal("newAdminAPIRuntime() returned nil")
	}
	if runtime.Config == nil {
		t.Fatal("runtime.Config is nil")
	}
	if runtime.Logger != logger {
		t.Fatal("runtime.Logger did not preserve logger")
	}
	if runtime.ServerAddr != "127.0.0.1:0" {
		t.Fatalf("runtime.ServerAddr = %q", runtime.ServerAddr)
	}
	if runtime.HTTPServer == nil {
		t.Fatal("runtime.HTTPServer is nil")
	}
	if runtime.HTTPServer.Addr != "127.0.0.1:0" {
		t.Fatalf("runtime.HTTPServer.Addr = %q", runtime.HTTPServer.Addr)
	}

	runtime.Close()
	if cleanupCalls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", cleanupCalls)
	}
}

func TestCleanupAdminAPIRuntimeBuildRunsInfraCleanupAndWrapsError(t *testing.T) {
	t.Parallel()

	cleanupCalls := 0
	cause := errors.New("boom")
	runtime, err := cleanupAdminAPIRuntimeBuild(&sharedmodules.InfraModule{
		Cleanup: func() {
			cleanupCalls++
		},
	}, "foundation", cause)

	if runtime != nil {
		t.Fatal("cleanupAdminAPIRuntimeBuild() returned runtime")
	}
	if !errors.Is(err, cause) {
		t.Fatalf("cleanupAdminAPIRuntimeBuild() error = %v", err)
	}
	if cleanupCalls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", cleanupCalls)
	}
}

func TestBotSettingsApplierMemberNewsRunNowPaths(t *testing.T) {
	t.Parallel()

	base := sharedsettings.NewLocalSettingsApplier(nil, nil, nil, nil)

	unconfigured := newBotSettingsApplier(base, nil, nil)
	unconfiguredResult := unconfigured.ApplyMemberNewsWeeklyRunNow(t.Context())
	if unconfiguredResult.Applied {
		t.Fatal("unconfigured member news trigger unexpectedly applied")
	}
	if unconfiguredResult.Reason != "member news trigger is not configured" {
		t.Fatalf("unconfigured reason = %q", unconfiguredResult.Reason)
	}

	trigger := &memberNewsRunNowStub{}
	configured := newBotSettingsApplier(base, trigger, slog.New(slog.DiscardHandler))
	successResult := configured.ApplyMemberNewsWeeklyRunNow(t.Context())
	if !successResult.Applied {
		t.Fatalf("configured result = %+v", successResult)
	}
	if successResult.Source != "member_news_trigger" {
		t.Fatalf("configured source = %q", successResult.Source)
	}
	if trigger.calls != 1 {
		t.Fatalf("trigger calls = %d, want 1", trigger.calls)
	}

	trigger.err = errors.New("trigger failed")
	failedResult := configured.ApplyMemberNewsWeeklyRunNow(t.Context())
	if failedResult.Applied {
		t.Fatal("failed trigger unexpectedly applied")
	}
	if failedResult.Reason != "member news trigger failed" {
		t.Fatalf("failed reason = %q", failedResult.Reason)
	}
	if failedResult.Error != "trigger failed" {
		t.Fatalf("failed error = %q", failedResult.Error)
	}
}

func TestBuildAdminAPISettingsApplierTriggerConfiguration(t *testing.T) {
	t.Parallel()

	foundation := &scraperHolodexProfileFoundation{}
	alarmMode := &alarmModeComponents{}
	ytStack := &providers.YouTubeStack{}
	logger := slog.New(slog.DiscardHandler)

	applier, triggerClient := buildAdminAPISettingsApplier(&config.Config{}, foundation, alarmMode, ytStack, logger)
	if applier == nil {
		t.Fatal("buildAdminAPISettingsApplier() returned nil applier")
	}
	if triggerClient != nil {
		t.Fatal("buildAdminAPISettingsApplier() returned trigger client for empty URL")
	}
	result := applier.ApplyMemberNewsWeeklyRunNow(t.Context())
	if result.Applied {
		t.Fatalf("empty URL member news result = %+v", result)
	}

	applier, triggerClient = buildAdminAPISettingsApplier(&config.Config{
		LLMSchedulerURL: "http://127.0.0.1:1",
		Server:          config.ServerConfig{APIKey: "test-key"},
	}, foundation, alarmMode, ytStack, logger)
	if applier == nil {
		t.Fatal("buildAdminAPISettingsApplier() returned nil applier with URL")
	}
	if triggerClient == nil {
		t.Fatal("buildAdminAPISettingsApplier() returned nil trigger client with URL")
	}
}

func TestBuildAdminAPIRouterAndHandlerHelpers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := slog.New(slog.DiscardHandler)
	appConfig := &config.Config{
		Server: config.ServerConfig{APIKey: "test-key"},
		CORS:   config.CORSConfig{AllowedOrigins: []string{"http://localhost:3000"}},
	}
	infra := &sharedmodules.InfraModule{
		Postgres: &databasemocks.Client{},
	}
	foundation := &scraperHolodexProfileFoundation{}
	alarmMode := &alarmModeComponents{}
	settingsApplier := sharedsettings.NewLocalSettingsApplier(nil, nil, nil, nil)

	handler := buildAdminHandler(
		appConfig,
		infra,
		foundation,
		alarmMode,
		nil,
		nil,
		nil,
		settingsApplier,
		buildAdminAPISystemCollector(appConfig),
		nil,
		nil,
		logger,
	)
	if handler == nil {
		t.Fatal("buildAdminHandler() returned nil")
	}

	router, err := buildAdminAPIRouter(t.Context(), appConfig, infra, nil, &server.Handler{}, logger)
	if err != nil {
		t.Fatalf("buildAdminAPIRouter() error = %v", err)
	}
	if router == nil {
		t.Fatal("buildAdminAPIRouter() returned nil router")
	}

	registerAdminAPIInternalAlarmRoutes(router, appConfig, alarmMode, logger)

	repository := buildAdminAPICommunityShortsOpsRepository(infra)
	if repository != nil {
		t.Fatal("buildAdminAPICommunityShortsOpsRepository() returned repository for nil pgx pool")
	}
}

func TestAdminAPIRuntimeLifecycleMethodsHandleNilAndNilServer(t *testing.T) {
	t.Parallel()

	var nilRuntime *AdminAPIRuntime
	nilRuntime.Run()
	nilRuntime.Start(t.Context(), nil)
	nilRuntime.StartHTTPServer(nil)
	nilRuntime.Shutdown(t.Context())
	if err := nilRuntime.ShutdownHTTPServer(t.Context()); err != nil {
		t.Fatalf("nil ShutdownHTTPServer() error = %v", err)
	}

	runtime := &AdminAPIRuntime{
		Logger:     slog.New(slog.DiscardHandler),
		ServerAddr: "127.0.0.1:0",
	}
	runtime.Start(t.Context(), make(chan error, 1))
	runtime.StartHTTPServer(make(chan error, 1))
	runtime.Shutdown(t.Context())
	if err := runtime.ShutdownHTTPServer(t.Context()); err != nil {
		t.Fatalf("ShutdownHTTPServer() error = %v", err)
	}
}
