package cache

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheServiceConnectsWithDirectTLSCredentials(t *testing.T) {
	t.Parallel()

	fixture := httptest.NewTLSServer(nil)
	certificate := fixture.TLS.Certificates[0]
	fixture.Close()

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), 0o600))

	server := miniredis.NewMiniRedis()
	require.NoError(t, server.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}}))
	t.Cleanup(server.Close)

	password := rand.Text()
	server.RequireUserAuth("cache", password)

	port, err := strconv.Atoi(server.Port())
	require.NoError(t, err)

	service, err := NewCacheService(t.Context(), Config{
		Host: server.Host(), Port: port, DB: 2, Username: "cache", Password: password, TLSCAFile: caFile, DisableCache: true,
	}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, service.Close()) })

	require.NoError(t, service.Set(t.Context(), "route", 42, 0))

	value, err := server.DB(2).Get("route")
	require.NoError(t, err)
	assert.Equal(t, "42", value)
}

func TestCacheClientDialsDevelopmentUnixSocket(t *testing.T) {
	t.Parallel()

	socket := filepath.Join(t.TempDir(), "valkey.sock")
	listener, err := new(net.ListenConfig).Listen(t.Context(), "unix", socket)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, listener.Close()) })

	opts, err := cacheClientOptions(Config{SocketPath: socket}, socket)
	require.NoError(t, err)
	require.NotNil(t, opts.DialCtxFn)

	conn, err := opts.DialCtxFn(t.Context(), socket, &opts.Dialer, nil)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, conn.Close()) })
	assert.Equal(t, "unix", conn.RemoteAddr().Network())
	assert.Equal(t, socket, conn.RemoteAddr().String())
}

func TestCacheServiceScanKeyPagesAdditional(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		want    []string
	}{
		{
			name:    "matching pattern returns expected keys",
			pattern: "scan:user:*",
			want:    []string{"scan:user:1", "scan:user:2"},
		},
		{
			name:    "non matching pattern returns empty",
			pattern: "scan:missing:*",
			want:    []string{},
		},
		{
			name:    "wildcard returns all keys",
			pattern: "*",
			want:    []string{"scan:user:1", "scan:user:2", "scan:stream:1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service, _ := newTestCacheService(t)
			ctx := t.Context()
			seedKeys := []string{"scan:user:1", "scan:user:2", "scan:stream:1"}

			for _, key := range seedKeys {
				require.NoError(t, service.Set(ctx, key, testPayload{Name: key}, 0))
			}

			var got []string

			err := service.ScanKeyPages(ctx, tt.pattern, 2, func(keys []string) error {
				require.NotEmpty(t, keys, "empty SCAN pages must not reach visit")

				got = append(got, keys...)

				return nil
			})

			require.NoError(t, err)
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

func TestCacheServiceScanKeyPagesStopsOnVisitError(t *testing.T) {
	t.Parallel()

	service, _ := newTestCacheService(t)
	ctx := t.Context()

	for i := range 10 {
		require.NoError(t, service.Set(ctx, fmt.Sprintf("scan:stop:%d", i), testPayload{Name: "stop"}, 0))
	}

	visitErr := errors.New("stop scan")
	visits := 0

	err := service.ScanKeyPages(ctx, "scan:stop:*", 1, func([]string) error {
		visits++

		return visitErr
	})

	require.ErrorIs(t, err, visitErr)
	assert.Equal(t, 1, visits, "a visit error must stop the scan before the next page")
}

func TestCacheServiceDelManyAdditional(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		seedKeys    []string
		deleteKeys  []string
		wantDeleted int64
		wantRemain  []string
	}{
		{
			name:        "deletes multiple keys successfully",
			seedKeys:    []string{"del:multi:a", "del:multi:b", "del:multi:c"},
			deleteKeys:  []string{"del:multi:a", "del:multi:b", "del:multi:c"},
			wantDeleted: 3,
		},
		{
			name:        "empty key list is no op",
			seedKeys:    []string{"del:empty:kept"},
			deleteKeys:  []string{},
			wantDeleted: 0,
			wantRemain:  []string{"del:empty:kept"},
		},
		{
			name:        "chunked delete removes all requested keys",
			seedKeys:    numberedKeys("del:chunk:", 1001),
			deleteKeys:  numberedKeys("del:chunk:", 1001),
			wantDeleted: 1001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service, _ := newTestCacheService(t)
			ctx := t.Context()

			for _, key := range tt.seedKeys {
				require.NoError(t, service.Set(ctx, key, testPayload{Name: key}, 0))
			}

			deleted, err := service.DelMany(ctx, tt.deleteKeys)

			require.NoError(t, err)
			assert.Equal(t, tt.wantDeleted, deleted)

			for _, key := range tt.deleteKeys {
				exists, existsErr := service.Exists(ctx, key)
				require.NoError(t, existsErr)
				assert.False(t, exists, "expected %s to be deleted", key)
			}

			for _, key := range tt.wantRemain {
				exists, existsErr := service.Exists(ctx, key)
				require.NoError(t, existsErr)
				assert.True(t, exists, "expected %s to remain", key)
			}
		})
	}
}

func TestCacheServiceExistsAdditional(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T, service *Service, ctx context.Context)
		key   string
		want  bool
	}{
		{
			name: "returns true for existing key",
			setup: func(t *testing.T, service *Service, ctx context.Context) {
				t.Helper()
				require.NoError(t, service.Set(ctx, "exists:present", testPayload{Name: testPayloadValue}, 0))
			},
			key:  "exists:present",
			want: true,
		},
		{
			name: "returns false for non existing key",
			key:  "exists:missing",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service, _ := newTestCacheService(t)
			ctx := t.Context()

			if tt.setup != nil {
				tt.setup(t, service, ctx)
			}

			got, err := service.Exists(ctx, tt.key)

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCacheServiceWaitUntilReadyTickAdditional(t *testing.T) {
	t.Parallel()

	t.Run("returns ready when connected", func(t *testing.T) {
		t.Parallel()

		service, _ := newTestCacheService(t)
		ctx := t.Context()
		ticks := make(chan time.Time, 1)

		ticks <- time.Now()

		ready, err := service.waitUntilReadyTick(ctx, ticks)

		require.NoError(t, err)
		assert.True(t, ready)
	})

	t.Run("returns error on context cancellation", func(t *testing.T) {
		t.Parallel()

		service, _ := newTestCacheService(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		ready, err := service.waitUntilReadyTick(ctx, make(chan time.Time))

		require.Error(t, err)
		assert.False(t, ready)
	})
}

func numberedKeys(prefix string, count int) []string {
	keys := make([]string, 0, count)
	for i := range count {
		keys = append(keys, fmt.Sprintf("%s%d", prefix, i))
	}

	return keys
}
