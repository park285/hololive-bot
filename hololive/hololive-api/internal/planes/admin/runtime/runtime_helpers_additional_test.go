package adminruntime

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/runtime/bootstrap"

	"github.com/kapu/hololive-api/internal/apifoundation"
	apiconfig "github.com/kapu/hololive-api/internal/config"
	server "github.com/kapu/hololive-api/internal/planes/admin/internal/server/api"
	sharedsettings "github.com/kapu/hololive-api/internal/server/settings"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	databasemocks "github.com/kapu/hololive-shared/pkg/service/database/mocks"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

func TestNormalizeRuntimeBuildInputsDefaultsTODOContext(t *testing.T) {
	t.Parallel()

	ctx, _, err := bootstrap.NormalizeRuntimeBuildInputs(t.Context(), &apiconfig.AdminPlaneConfig{}, slog.New(slog.DiscardHandler))
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

	runtime, err := newAdminAPIRuntime(t.Context(), &apiconfig.AdminPlaneConfig{
		Server: settings.ServerConfig{
			HTTPTransports: []string{"h3"},
			H3Addr:         testLoopbackAddr,
			H3CertFile:     certFile,
			H3KeyFile:      keyFile,
		},
	}, logger, gin.New(), func() error {
		cleanupCalls++
		return nil
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

	if runtime.ServerAddr != testLoopbackAddr {
		t.Fatalf("runtime.ServerAddr = %q", runtime.ServerAddr)
	}

	if runtime.HTTPServers == nil || runtime.HTTPServers.H3 == nil {
		t.Fatal("runtime.HTTPServers.H3 is nil")
	}

	if runtime.HTTPServers.H3.Addr != testLoopbackAddr {
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

func TestBuildAdminAPIHTTPRuntimeCleansUpInfraOnRouterFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cleanupCalls := 0
	infra := &sharedmodules.InfraModule{
		Postgres: &databasemocks.Client{},
		Cleanup: func() {
			cleanupCalls++
		},
	}

	runtime, err := buildAdminAPIHTTPRuntime(
		t.Context(),
		&apiconfig.AdminPlaneConfig{},
		infra,
		&server.Handler{},
		func() error {
			infra.Cleanup()

			return nil
		},
		slog.New(slog.DiscardHandler),
	)
	if err == nil {
		t.Fatal("buildAdminAPIHTTPRuntime() expected error for blank API key")
	}

	if runtime != nil {
		t.Fatal("buildAdminAPIHTTPRuntime() returned runtime on error")
	}

	if !strings.Contains(err.Error(), "build admin api runtime: provide api router: ") {
		t.Fatalf("buildAdminAPIHTTPRuntime() error = %q", err.Error())
	}

	if cleanupCalls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", cleanupCalls)
	}
}

func TestBuildAdminAPIRouterAndHandlerHelpers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := slog.New(slog.DiscardHandler)
	appConfig := &apiconfig.AdminPlaneConfig{
		Server: settings.ServerConfig{APIKey: testAPIKey},
		CORS:   settings.CORSConfig{AllowedOrigins: []string{testAllowedOrigin}},
	}
	infra := &sharedmodules.InfraModule{
		Postgres: &databasemocks.Client{},
		// /api/holo rate limit은 cache가 없으면 기동 오류다(fail-closed).
		Cache: sharedtestutil.NewTestCacheService(t.Context(), t),
	}
	foundation := &apifoundation.ScraperHolodexFoundation{}
	alarmMode := &alarmModeComponents{}
	settingsApplier := sharedsettings.NewLocalSettingsApplier(nil)

	settingsService, err := sharedsettings.BuildSettingsService(filepath.Join(t.TempDir(), "settings.json"), nil, logger)
	if err != nil {
		t.Fatalf("BuildSettingsService() error = %v", err)
	}

	handler := buildAdminHandler(
		infra,
		foundation,
		alarmMode,
		nil,
		nil,
		settingsService,
		settingsApplier,
		logger,
	)
	if handler == nil {
		t.Fatal("buildAdminHandler() returned nil")
	}

	router, err := buildAdminAPIRouter(t.Context(), appConfig, infra, &server.Handler{}, logger)
	if err != nil {
		t.Fatalf("buildAdminAPIRouter() error = %v", err)
	}

	if router == nil {
		t.Fatal("buildAdminAPIRouter() returned nil router")
	}
}

func TestAdminAPIRuntimeLifecycleMethodsHandleNilAndNilServer(t *testing.T) {
	t.Parallel()

	var nilRuntime *AdminAPIRuntime

	if err := nilRuntime.Run(); err != nil {
		t.Fatalf("nil Run() error = %v", err)
	}

	nilRuntime.Start(t.Context(), nil)
	nilRuntime.StartHTTPServer(nil)

	if err := nilRuntime.Shutdown(t.Context()); err != nil {
		t.Fatalf("nil Shutdown() error = %v", err)
	}

	if err := nilRuntime.ShutdownHTTPServer(t.Context()); err != nil {
		t.Fatalf("nil ShutdownHTTPServer() error = %v", err)
	}

	runtime := &AdminAPIRuntime{
		Logger:     slog.New(slog.DiscardHandler),
		ServerAddr: testLoopbackAddr,
	}
	runtime.Start(t.Context(), make(chan error, 1))
	runtime.StartHTTPServer(make(chan error, 1))

	if err := runtime.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	if err := runtime.ShutdownHTTPServer(t.Context()); err != nil {
		t.Fatalf("ShutdownHTTPServer() error = %v", err)
	}
}
