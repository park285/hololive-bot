package delivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/park285/iris-client-go/v4/iris"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

const (
	runtimeIrisReplyRetryMax          = 3
	runtimeIrisBaseURLResolveInterval = time.Second
)

var errRuntimeIrisClientClosed = errors.New("runtime iris client: client is closed")

// RuntimeIrisClient는 IRIS_BASE_URL_FILE 회전을 따라 Iris client를 다시 묶는다.
//
// 회전으로 교체된 client는 시간 grace가 아니라 in-flight 참조 수로 닫는다. 각 호출은 시작할 때
// 현재 binding을 acquire하고 끝날 때 release하며, current 자리도 참조 하나를 가진다. 그래서 교체
// 뒤 마지막 호출이 release하는 순간에만 이전 client가 닫히고, retry를 포함해 진행 중인 요청은
// 끊기지 않는다. Refresh는 호출 goroutine에서 single-flight로 실행하며 resolver panic을 복구하지 않는다.
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

// runtimeIrisBinding은 한 Base URL에 묶인 client와 참조 수다. 참조 수는 mu 아래에서만 바꾼다.
type runtimeIrisBinding struct {
	baseURL string
	client  *iris.APIClient
	refs    int
}

var _ iris.BotClient = (*RuntimeIrisClient)(nil)

// NewRuntimeIrisClient는 typed Iris 설정으로 client를 만든다. 빈 token, 빈 Base URL 원천, 초기 Base URL
// 해석·client 생성 오류는 지연하지 않고 즉시 반환한다. 옵션 opts는 기본 옵션 뒤에 적용된다.
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

// acquire는 현재 binding의 참조를 하나 늘려 반환한다. 해석 snapshot이 만료됐으면 single-flight로
// refresh한다. 다른 호출이 refresh 중이면 끝날 때까지 기다린 뒤 다시 확인한다.
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

// refresh는 호출 goroutine에서 Base URL을 해석하고 필요하면 client를 교체한다. Defer는 대기자를 깨우기만
// 하고 panic을 복구하지 않으므로, resolver나 client 생성의 panic은 refresh를 시작한 호출자에게 전파된다.
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

	// 해석한 Base URL을 마지막에 적용해 호출자 옵션이 회전 대상을 덮지 못하게 한다.
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

// dropRefLocked는 참조 하나를 내리고, 마지막 참조였으면 true를 반환한다.
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

// Close는 이후 호출을 거절한다. 진행 중인 호출이 있으면 현재 client는 마지막 release가 닫는다.
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

// 위임 메서드는 시그니처가 달라 공통 헬퍼로 합치지 않는다. 각 메서드는 binding을 acquire해 호출이
// 끝날 때까지 참조를 유지하고, SDK 오류는 %w로 감싸 errors.Is 분류(ErrNotSent·ErrTransport 등)를 보존한다.

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

func (c *RuntimeIrisClient) SendMarkdown(ctx context.Context, room, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	binding, err := c.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer c.release(binding)

	out, err := binding.client.SendMarkdown(ctx, room, markdown, opts...)
	if err != nil {
		return nil, fmt.Errorf("send markdown: %w", err)
	}

	return out, nil
}

// SendRichText는 Markdown 본문을 Iris가 카카오 네이티브 서식으로 변환해 보내게 한다.
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

// Ping은 GET /ready를 한 번 확인한다. 재시도와 로그는 호출자가 소유한다.
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
