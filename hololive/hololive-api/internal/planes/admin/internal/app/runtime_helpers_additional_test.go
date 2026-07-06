package app

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"

	"github.com/park285/shared-go/pkg/runtime/bootstrap"

	"github.com/kapu/hololive-api/internal/planes/admin/internal/server"
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
	certFile, keyFile := writeRuntimeTestCertificate(t)
	runtime, err := newAdminAPIRuntime(&config.Config{
		Server: config.ServerConfig{
			HTTPTransports: []string{"h3"},
			H3Addr:         "127.0.0.1:0",
			H3CertFile:     certFile,
			H3KeyFile:      keyFile,
		},
	}, logger, gin.New(), func() {
		cleanupCalls++
	})
	if err != nil {
		t.Fatalf("newAdminAPIRuntime() error = %v", err)
	}

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
	if runtime.HTTPServers == nil || runtime.HTTPServers.H3 == nil {
		t.Fatal("runtime.HTTPServers.H3 is nil")
	}
	if runtime.HTTPServers.H3.Addr != "127.0.0.1:0" {
		t.Fatalf("runtime.HTTPServers.H3.Addr = %q", runtime.HTTPServers.H3.Addr)
	}

	runtime.Close()
	if cleanupCalls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", cleanupCalls)
	}
}

func writeRuntimeTestCertificate(t *testing.T) (certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		DNSNames:     []string{"localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	dir := t.TempDir()
	certFile = filepath.Join(dir, "cert.pem")
	keyFile = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certFile, keyFile
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
