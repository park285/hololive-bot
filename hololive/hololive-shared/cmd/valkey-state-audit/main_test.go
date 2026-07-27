package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/stretchr/testify/require"
)

type stubAuditor struct {
	report   cache.PersistedStateAudit
	closed   bool
	closeErr error
}

func (s *stubAuditor) AuditPersistedState(context.Context) (cache.PersistedStateAudit, error) {
	return s.report, nil
}

func (s *stubAuditor) Close() error {
	s.closed = true
	return s.closeErr
}

func TestRunReturnsCloseError(t *testing.T) {
	auditor := &stubAuditor{closeErr: errors.New("close failed")}
	connect := func(_ context.Context, _ cache.Config, _ *slog.Logger) (persistedStateAuditor, error) {
		return auditor, nil
	}

	err := run(t.Context(), &bytes.Buffer{}, func() settings.ValkeyConfig { return settings.ValkeyConfig{} }, connect)
	require.ErrorContains(t, err, "close valkey state audit connection: close failed")
	require.True(t, auditor.closed)
}

func TestRunUsesCanonicalCacheEnvironment(t *testing.T) {
	t.Setenv("CACHE_HOST", "cache.internal")
	t.Setenv("CACHE_PORT", "6381")
	t.Setenv("CACHE_PASSWORD", " raw password ")
	t.Setenv("CACHE_DB", "7")
	t.Setenv("CACHE_SOCKET_PATH", "/run/valkey/valkey.sock")

	wantReport := cache.PersistedStateAudit{
		CanonicalNotifiedKeys:       3,
		AggregateNotifiedLegacyKeys: 1,
		MemberHashMissing:           1,
	}
	auditor := &stubAuditor{report: wantReport}
	var connectedConfig cache.Config
	connect := func(_ context.Context, config cache.Config, _ *slog.Logger) (persistedStateAuditor, error) {
		connectedConfig = config
		return auditor, nil
	}

	var output bytes.Buffer
	err := run(t.Context(), &output, settings.LoadValkeyConfig, connect)
	require.NoError(t, err)
	require.Equal(t, cache.Config{
		Host:              "cache.internal",
		Port:              6381,
		Password:          " raw password ",
		DB:                7,
		SocketPath:        "/run/valkey/valkey.sock",
		DisableCache:      true,
		ForceSingleClient: true,
	}, connectedConfig)
	require.True(t, auditor.closed)

	var gotReport cache.PersistedStateAudit
	require.NoError(t, json.Unmarshal(output.Bytes(), &gotReport))
	require.Equal(t, wantReport, gotReport)
	require.Contains(t, output.String(), `"aggregate_notified_legacy_keys":1`)
	require.Contains(t, output.String(), `"old_member_cache_keys":0`)
	require.NotContains(t, output.String(), "cache.internal")
	require.NotContains(t, output.String(), "raw password")
}
