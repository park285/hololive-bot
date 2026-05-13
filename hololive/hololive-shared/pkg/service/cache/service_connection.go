package cache

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func NewCacheService(ctx context.Context, cfg Config, logger *slog.Logger) (*Service, error) {
	var addr string
	var connMethod string
	if cfg.SocketPath != "" {
		addr = cfg.SocketPath
		connMethod = "unix"
	} else {
		addr = net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
		connMethod = "tcp"
	}

	opts := valkey.ClientOption{
		InitAddress:       []string{addr},
		Password:          cfg.Password,
		SelectDB:          cfg.DB,
		ConnWriteTimeout:  constants.MQConfig.ConnWriteTimeout,
		BlockingPoolSize:  constants.ValkeyConfig.BlockingPoolSize,
		PipelineMultiplex: constants.ValkeyConfig.PipelineMultiplex,
		DisableCache:      cfg.DisableCache,
		ForceSingleClient: cfg.ForceSingleClient,
	}

	if cfg.SocketPath != "" {
		socketPath := cfg.SocketPath
		opts.DialCtxFn = func(ctx context.Context, _ string, _ *net.Dialer, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			d.Timeout = constants.MQConfig.DialTimeout
			return d.DialContext(ctx, "unix", socketPath)
		}
	} else {
		opts.Dialer = net.Dialer{Timeout: constants.MQConfig.DialTimeout}
	}

	client, err := valkey.NewClient(opts)
	if err != nil {
		return nil, NewCacheError("failed to create cache client", "init", "", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, constants.ValkeyConfig.ReadyTimeout)
	defer cancel()

	if err := client.Do(pingCtx, client.B().Ping().Build()).Error(); err != nil {
		client.Close()
		return nil, NewCacheError("failed to connect to cache store", "ping", "", err)
	}

	logger.Info("Cache store connected",
		slog.String("addr", addr),
		slog.String("method", connMethod),
		slog.Int("db", cfg.DB),
		slog.Int("pool_size", constants.ValkeyConfig.BlockingPoolSize),
	)

	return &Service{
		client: client,
		logger: logger,
	}, nil
}

func (c *Service) Close() error {
	var closeErr error

	c.closeOnce.Do(func() {
		if c.client == nil {
			return
		}

		c.client.Close()
		c.logger.Info("Cache store disconnected")
	})

	return closeErr
}

func (c *Service) IsConnected(ctx context.Context) bool {
	return c.client.Do(ctx, c.client.B().Ping().Build()).Error() == nil
}

func (c *Service) WaitUntilReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	return c.waitUntilReady(ctx, ticker.C)
}

func (c *Service) waitUntilReady(ctx context.Context, ticks <-chan time.Time) error {
	for {
		ready, err := c.waitUntilReadyTick(ctx, ticks)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
	}
}

func (c *Service) waitUntilReadyTick(ctx context.Context, ticks <-chan time.Time) (bool, error) {
	select {
	case <-ctx.Done():
		return false, fmt.Errorf("timeout waiting for cache store to be ready")
	case <-ticks:
		return c.IsConnected(ctx), nil
	}
}

func (c *Service) GetClient() valkey.Client {
	return c.client
}

func (c *Service) DoMulti(ctx context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult {
	if len(cmds) == 0 {
		return nil
	}
	return c.client.DoMulti(ctx, cmds...)
}

func (c *Service) Builder() valkey.Builder {
	return c.client.B()
}

// B: 명령 빌더를 반환합니다.
func (c *Service) B() valkey.Builder {
	return c.client.B()
}
