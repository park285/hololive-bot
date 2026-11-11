package collectorruntime

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	collectorconfig "github.com/kapu/hololive-youtube-collector/internal/config"
)

func TestCollectorDatabaseStartupRecoversWithinBudget(t *testing.T) {
	pool := dbtest.NewBlankPool(t)
	config := startupDatabaseConfig(pool)
	proxy := newStartupDatabaseProxy(t, net.JoinHostPort(config.Postgres.Host, strconv.Itoa(config.Postgres.Port)))

	config.Postgres.Host = "127.0.0.1"
	config.Postgres.Port = proxy.port

	observer := &startupDatabaseLogHandler{Handler: slog.DiscardHandler, ready: &proxy.ready}
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)

	defer cancel()

	resources, cleanup, err := provideCollectorDatabase(ctx, config, slog.New(observer), true)
	if err != nil {
		t.Fatalf("initial database did not recover: %T", err)
	}

	t.Cleanup(cleanup)

	var answer int

	if err := resources.Service.GetPool().QueryRow(ctx, "SELECT 42").Scan(&answer); err != nil || answer != 42 {
		t.Fatalf("recovered database query: answer=%d error_type=%T", answer, err)
	}

	if observer.waits.Load() != 1 || ctx.Err() != nil {
		t.Fatalf("startup waits=%d context=%v", observer.waits.Load(), ctx.Err())
	}

	cleanup()
	proxy.requireClosed(t)
}

func TestCollectorDatabaseStartupSingleAttemptRemainsDefault(t *testing.T) {
	config := startupDatabaseConfig(dbtest.NewBlankPool(t))
	proxy := newStartupDatabaseProxy(t, net.JoinHostPort(config.Postgres.Host, strconv.Itoa(config.Postgres.Port)))

	config.Postgres.Host = "127.0.0.1"
	config.Postgres.Port = proxy.port

	observer := &startupDatabaseLogHandler{Handler: slog.DiscardHandler, ready: &proxy.ready}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)

	defer cancel()

	resources, cleanup, err := provideCollectorDatabase(ctx, config, slog.New(observer), false)
	if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil || resources != nil || cleanup != nil || observer.waits.Load() != 0 {
		t.Fatalf("single attempt: error_type=%T context=%v resources=%t cleanup=%t waits=%d", err, ctx.Err(), resources != nil, cleanup != nil, observer.waits.Load())
	}

	proxy.requireClosed(t)
}

func TestCollectorDatabaseStartupCancellationStopsWaiting(t *testing.T) {
	config := startupDatabaseConfig(dbtest.NewBlankPool(t))
	proxy := newStartupDatabaseProxy(t, net.JoinHostPort(config.Postgres.Host, strconv.Itoa(config.Postgres.Port)))

	config.Postgres.Host = "127.0.0.1"
	config.Postgres.Port = proxy.port

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)

	defer cancel()

	observer := &startupDatabaseLogHandler{Handler: slog.DiscardHandler, cancel: cancel}

	resources, cleanup, err := provideCollectorDatabase(ctx, config, slog.New(observer), true)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) || resources != nil || cleanup != nil || observer.waits.Load() != 1 {
		t.Fatalf("canceled startup: error_type=%T resources=%t cleanup=%t waits=%d", err, resources != nil, cleanup != nil, observer.waits.Load())
	}

	proxy.requireClosed(t)
}

func TestCollectorDatabaseStartupRejectsNonTimeoutFailures(t *testing.T) {
	pool := dbtest.NewBlankPool(t)

	for _, scenario := range []string{"authentication", "tls", "configuration"} {
		t.Run(scenario, func(t *testing.T) {
			config := startupDatabaseConfig(pool)

			switch scenario {
			case "authentication":
				config.Postgres.Password = rand.Text()
			case "tls":
				config.Postgres.SSLMode = "require"
			case "configuration":
				config.Postgres.SSLMode = "invalid-mode"
			}

			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()

			observer := &startupDatabaseLogHandler{Handler: slog.DiscardHandler}

			resources, cleanup, err := provideCollectorDatabase(ctx, config, slog.New(observer), true)
			if err == nil || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil || resources != nil || cleanup != nil || observer.waits.Load() != 0 {
				t.Fatalf("non-timeout failure: error_type=%T context=%v resources=%t cleanup=%t waits=%d", err, ctx.Err(), resources != nil, cleanup != nil, observer.waits.Load())
			}

			if scenario == "authentication" {
				if postgresError, ok := errors.AsType[*pgconn.PgError](err); !ok || postgresError.Code != "28P01" {
					t.Fatalf("authentication failure lost SQLSTATE: %T", err)
				}
			}
		})
	}
}

func startupDatabaseConfig(pool *pgxpool.Pool) *collectorconfig.RuntimeConfig {
	connection := pool.Config().ConnConfig

	return &collectorconfig.RuntimeConfig{Postgres: settings.PostgresConfig{
		Host: connection.Host, Port: int(connection.Port), User: connection.User, Password: connection.Password,
		Database: connection.Database, SSLMode: "disable", PoolMinConns: 1, PoolMaxConns: 2,
	}}
}

type startupDatabaseLogHandler struct {
	slog.Handler

	ready  *atomic.Bool
	cancel context.CancelFunc
	waits  atomic.Int32
}

func (h *startupDatabaseLogHandler) Handle(_ context.Context, record slog.Record) error {
	if record.Level == slog.LevelWarn {
		h.waits.Add(1)

		if h.ready != nil {
			h.ready.Store(true)
		}

		if h.cancel != nil {
			h.cancel()
		}
	}

	return nil
}

func (h *startupDatabaseLogHandler) Enabled(context.Context, slog.Level) bool { return true }

type startupDatabaseProxy struct {
	t           *testing.T
	port        int
	listener    net.Listener
	backend     string
	ready       atomic.Bool
	accepted    chan struct{}
	connections sync.WaitGroup
}

func newStartupDatabaseProxy(t *testing.T, backend string) *startupDatabaseProxy {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for isolated database: %T", err)
	}

	t.Cleanup(func() { closeStartupDatabaseTransport(t, listener) })

	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("isolated database listener is not TCP")
	}

	proxy := &startupDatabaseProxy{t: t, port: address.Port, listener: listener, backend: backend, accepted: make(chan struct{})}

	go func() {
		defer close(proxy.accepted)

		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}

			proxy.connections.Add(1)

			go proxy.serve(t.Context(), connection)
		}
	}()

	return proxy
}

func (p *startupDatabaseProxy) serve(ctx context.Context, connection net.Conn) {
	defer p.connections.Done()
	defer closeStartupDatabaseTransport(p.t, connection)

	stop := context.AfterFunc(ctx, func() { closeStartupDatabaseTransport(p.t, connection) })
	defer stop()

	if !p.ready.Load() {
		copyStartupDatabaseTraffic(p.t, io.Discard, connection)

		return
	}

	upstream, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", p.backend)
	if err != nil {
		p.t.Errorf("isolated database upstream failed: %T", err)

		return
	}
	defer closeStartupDatabaseTransport(p.t, upstream)

	copied := make(chan struct{})

	go func() {
		copyStartupDatabaseTraffic(p.t, upstream, connection)
		closeStartupDatabaseTransport(p.t, upstream)
		close(copied)
	}()

	copyStartupDatabaseTraffic(p.t, connection, upstream)
	closeStartupDatabaseTransport(p.t, connection)
	<-copied
}

func (p *startupDatabaseProxy) requireClosed(t *testing.T) {
	t.Helper()
	closeStartupDatabaseTransport(t, p.listener)
	<-p.accepted

	done := make(chan struct{})

	go func() {
		p.connections.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("database pool cleanup left an open connection")
	}
}

func closeStartupDatabaseTransport(t *testing.T, transport io.Closer) {
	t.Helper()

	if err := transport.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Errorf("isolated database transport close failed: %T", err)
	}
}

func copyStartupDatabaseTraffic(t *testing.T, destination io.Writer, source io.Reader) {
	t.Helper()

	if _, err := io.Copy(destination, source); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Errorf("isolated database transport copy failed: %T", err)
	}
}
