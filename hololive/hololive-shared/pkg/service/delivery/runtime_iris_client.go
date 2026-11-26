package delivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/park285/iris-client-go/v6/iris"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

const (
	runtimeIrisReplyRetryMax          = 3
	runtimeIrisBaseURLResolveInterval = time.Second
)

var errRuntimeIrisClientClosed = errors.New("runtime iris client: client is closed")

type RuntimeIrisClient struct {
	resolveBaseURL func() (string, error)
	h3DialGuard    *runtimeIrisH3DialGuard
	clientOpts     []iris.ClientOption
	logger         *slog.Logger
	now            func() time.Time

	mu                sync.Mutex
	current           *runtimeIrisBinding
	resolveValidUntil time.Time
	resolveErr        error
	refreshing        chan struct{}
	closed            bool
}

type runtimeIrisBinding struct {
	baseURL string
	client  *iris.APIClient
	refs    int
}

var _ iris.BotClient = (*RuntimeIrisClient)(nil)

func NewRuntimeIrisClient(ctx context.Context, config settings.IrisConfig, logger *slog.Logger, opts ...iris.ClientOption) (*RuntimeIrisClient, error) {
	if logger == nil {
		logger = slog.Default()
	}

	botToken := strings.TrimSpace(config.BotToken)
	if botToken == "" {
		return nil, errors.New("runtime iris client: bot token is required")
	}

	resolver, err := newRuntimeIrisBaseURLResolver(config)
	if err != nil {
		return nil, fmt.Errorf("runtime iris client: %w", err)
	}

	h3DialGuard := newRuntimeIrisH3DialGuard(resolver.resolve, logger)

	clientOpts := []iris.ClientOption{
		iris.WithBotToken(botToken),
		iris.WithTransport(config.Transport),
		iris.WithReplyRetry(runtimeIrisReplyRetryMax),
		iris.WithH3DialGuard(h3DialGuard.allow),
	}

	if caFile := strings.TrimSpace(config.H3CACertFile); caFile != "" {
		clientOpts = append(clientOpts, iris.WithH3CACertFile(caFile))
	}

	if serverName := strings.TrimSpace(config.H3ServerName); serverName != "" {
		clientOpts = append(clientOpts, iris.WithH3ServerName(serverName))
	}

	c := &RuntimeIrisClient{
		resolveBaseURL: resolver.resolve,
		h3DialGuard:    h3DialGuard,
		clientOpts:     append(clientOpts, opts...),
		logger:         logger,
		now:            time.Now,
	}

	binding, err := c.acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("runtime iris client: initial bind: %w", err)
	}

	c.release(binding)

	return c, nil
}

func (c *RuntimeIrisClient) acquire(ctx context.Context) (*runtimeIrisBinding, error) {
	for {
		c.mu.Lock()

		if c.closed {
			c.mu.Unlock()

			return nil, errRuntimeIrisClientClosed
		}

		if err := ctx.Err(); err != nil {
			c.mu.Unlock()

			return nil, fmt.Errorf("acquire iris client: %w", err)
		}

		if c.snapshotFreshLocked() {
			if c.resolveErr != nil {
				err := c.resolveErr
				c.mu.Unlock()

				return nil, err
			}

			binding := c.current
			binding.refs++
			c.mu.Unlock()

			return binding, nil
		}

		if waiting := c.refreshing; waiting != nil {
			c.mu.Unlock()

			select {
			case <-waiting:
				continue
			case <-ctx.Done():
				return nil, fmt.Errorf("wait iris client refresh: %w", ctx.Err())
			}
		}

		done := make(chan struct{})

		c.refreshing = done
		c.mu.Unlock()

		c.refresh(done)
	}
}

func (c *RuntimeIrisClient) snapshotFreshLocked() bool {
	return !c.resolveValidUntil.IsZero() && c.now().Before(c.resolveValidUntil) && (c.resolveErr != nil || c.current != nil)
}

func (c *RuntimeIrisClient) refresh(done chan struct{}) {
	var (
		baseURL string
		next    *iris.APIClient
		err     error
	)

	defer func() {
		c.mu.Lock()

		c.refreshing = nil
		c.mu.Unlock()
		close(done)
	}()

	baseURL, err = c.resolveBaseURL()
	if err != nil {
		c.storeSnapshot(fmt.Errorf("resolve base URL: %w", err))

		return
	}

	c.mu.Lock()

	unchanged := c.current != nil && c.current.baseURL == baseURL
	c.mu.Unlock()

	if unchanged {
		c.storeSnapshot(nil)

		return
	}

	opts := make([]iris.ClientOption, 0, len(c.clientOpts)+1)

	opts = append(opts, c.clientOpts...)
	opts = append(opts, iris.WithBaseURL(baseURL))

	next, err = iris.NewClient(opts...)
	if err != nil {
		c.storeSnapshot(fmt.Errorf("create client: %w", err))

		return
	}

	c.mu.Lock()

	if c.closed {
		c.mu.Unlock()
		c.closeClient(next)

		return
	}

	previous := c.current

	c.current = &runtimeIrisBinding{baseURL: baseURL, client: next, refs: 1}
	c.markSnapshotLocked(nil)

	closePrevious := c.dropRefLocked(previous)
	c.mu.Unlock()

	if closePrevious {
		c.closeClient(previous.client)
	}
}

func (c *RuntimeIrisClient) storeSnapshot(err error) {
	c.mu.Lock()
	c.markSnapshotLocked(err)
	c.mu.Unlock()
}

func (c *RuntimeIrisClient) markSnapshotLocked(err error) {
	c.resolveValidUntil = c.now().Add(runtimeIrisBaseURLResolveInterval)
	c.resolveErr = err
}

func (c *RuntimeIrisClient) dropRefLocked(binding *runtimeIrisBinding) bool {
	if binding == nil {
		return false
	}

	binding.refs--

	return binding.refs == 0
}

func (c *RuntimeIrisClient) release(binding *runtimeIrisBinding) {
	c.mu.Lock()

	last := c.dropRefLocked(binding)
	c.mu.Unlock()

	if last {
		c.closeClient(binding.client)
	}
}

func (c *RuntimeIrisClient) closeClient(client *iris.APIClient) {
	if err := client.Close(); err != nil {
		c.logger.Warn("runtime_iris_client_close_stale_failed", slog.Any("error", err))
	}
}

func (c *RuntimeIrisClient) Close() error {
	c.mu.Lock()

	if c.closed {
		c.mu.Unlock()

		return nil
	}

	c.closed = true

	current := c.current

	c.current = nil

	last := c.dropRefLocked(current)
	c.mu.Unlock()

	if !last {
		return nil
	}

	if err := current.client.Close(); err != nil {
		return fmt.Errorf("close iris client: %w", err)
	}

	return nil
}

func (c *RuntimeIrisClient) SendMessageAccepted(ctx context.Context, room, message string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	binding, err := c.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer c.release(binding)

	out, err := binding.client.SendMessageAccepted(ctx, room, message, opts...)
	if err != nil {
		return nil, fmt.Errorf("send message: %w", err)
	}

	return out, nil
}

func (c *RuntimeIrisClient) SendImage(ctx context.Context, room string, imageData []byte, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	binding, err := c.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer c.release(binding)

	out, err := binding.client.SendImage(ctx, room, imageData, opts...)
	if err != nil {
		return nil, fmt.Errorf("send image: %w", err)
	}

	return out, nil
}

func (c *RuntimeIrisClient) SendFile(ctx context.Context, room string, file iris.ReplyFile, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	binding, err := c.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer c.release(binding)

	out, err := binding.client.SendFile(ctx, room, file, opts...)
	if err != nil {
		return nil, fmt.Errorf("send file: %w", err)
	}

	return out, nil
}

func (c *RuntimeIrisClient) SendMultipleImages(ctx context.Context, room string, images [][]byte, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	binding, err := c.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer c.release(binding)

	out, err := binding.client.SendMultipleImages(ctx, room, images, opts...)
	if err != nil {
		return nil, fmt.Errorf("send multiple images: %w", err)
	}

	return out, nil
}

func (c *RuntimeIrisClient) SendRichText(ctx context.Context, room, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	binding, err := c.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer c.release(binding)

	out, err := binding.client.SendRichText(ctx, room, markdown, opts...)
	if err != nil {
		return nil, fmt.Errorf("send rich text: %w", err)
	}

	return out, nil
}

func (c *RuntimeIrisClient) GetReplyStatus(ctx context.Context, requestID string) (*iris.ReplyStatusSnapshot, error) {
	binding, err := c.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer c.release(binding)

	out, err := binding.client.GetReplyStatus(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("get reply status: %w", err)
	}

	return out, nil
}

func (c *RuntimeIrisClient) Ping(ctx context.Context) error {
	binding, err := c.acquire(ctx)
	if err != nil {
		return err
	}
	defer c.release(binding)

	if err := binding.client.Ping(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}

	return nil
}

func (c *RuntimeIrisClient) GetRooms(ctx context.Context) (*iris.RoomListResponse, error) {
	binding, err := c.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer c.release(binding)

	out, err := binding.client.GetRooms(ctx)
	if err != nil {
		return nil, fmt.Errorf("get rooms: %w", err)
	}

	return out, nil
}
