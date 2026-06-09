package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/park285/iris-client-go/iris"
)

const defaultStaleClientCloseGrace = 30 * time.Second

type RuntimeIrisClient struct {
	fallbackBaseURL string
	botToken        string
	baseURLFilePath string
	logger          *slog.Logger
	clientOpts      []iris.ClientOption
	staleCloseGrace time.Duration

	mu                             sync.Mutex
	baseURLHostUnvalidatedWarnOnce sync.Once
	cachedBaseURL                  string
	cachedH2CClient                *iris.H2CClient
	closed                         bool
	closeSignal                    chan struct{}
	staleClosers                   sync.WaitGroup
}

func NewRuntimeIrisClient(
	fallbackBaseURL string,
	botToken string,
	baseURLFilePath string,
	logger *slog.Logger,
	opts ...iris.ClientOption,
) *RuntimeIrisClient {
	if logger == nil {
		logger = slog.Default()
	}

	clientOpts := make([]iris.ClientOption, 0, len(opts)+2)
	clientOpts = append(clientOpts, iris.WithLogger(logger))
	clientOpts = append(clientOpts, iris.WithReplyRetry(3))
	clientOpts = append(clientOpts, opts...)

	return &RuntimeIrisClient{
		fallbackBaseURL: strings.TrimSpace(fallbackBaseURL),
		botToken:        strings.TrimSpace(botToken),
		baseURLFilePath: strings.TrimSpace(baseURLFilePath),
		logger:          logger,
		clientOpts:      clientOpts,
		staleCloseGrace: defaultStaleClientCloseGrace,
		closeSignal:     make(chan struct{}),
	}
}

func (c *RuntimeIrisClient) SendMessage(ctx context.Context, room, message string, opts ...iris.SendOption) error {
	client, err := c.currentClient()
	if err != nil {
		return err
	}
	return client.SendMessage(ctx, room, message, opts...)
}

func (c *RuntimeIrisClient) SendMessageWithClientRequestID(ctx context.Context, room, message, clientRequestID string) error {
	return c.SendMessage(ctx, room, message, iris.WithClientRequestID(clientRequestID))
}

func (c *RuntimeIrisClient) SendImage(ctx context.Context, room string, imageData []byte, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.SendImage(ctx, room, imageData, opts...)
}

func (c *RuntimeIrisClient) SendMultipleImages(ctx context.Context, room string, images [][]byte, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.SendMultipleImages(ctx, room, images, opts...)
}

func (c *RuntimeIrisClient) SendMarkdown(ctx context.Context, room, markdown string, opts ...iris.SendOption) (*iris.ReplyAcceptedResponse, error) {
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.SendMarkdown(ctx, room, markdown, opts...)
}

func (c *RuntimeIrisClient) GetReplyStatus(ctx context.Context, requestID string) (*iris.ReplyStatusSnapshot, error) {
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.GetReplyStatus(ctx, requestID)
}

func (c *RuntimeIrisClient) Ping(ctx context.Context) bool {
	client, err := c.currentClient()
	if err != nil {
		return false
	}
	return client.Ping(ctx)
}

func (c *RuntimeIrisClient) GetConfig(ctx context.Context) (*iris.ConfigResponse, error) {
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.GetConfig(ctx)
}

func (c *RuntimeIrisClient) UpdateConfig(ctx context.Context, name string, req iris.ConfigUpdateRequest) (*iris.ConfigUpdateResponse, error) {
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.UpdateConfig(ctx, name, req)
}

func (c *RuntimeIrisClient) GetBridgeHealth(ctx context.Context) (*iris.BridgeHealthResult, error) {
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.GetBridgeHealth(ctx)
}

func (c *RuntimeIrisClient) GetNativeCoreDiagnostics(ctx context.Context) (*iris.NativeCoreDiagnostics, error) {
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.GetNativeCoreDiagnostics(ctx)
}

func (c *RuntimeIrisClient) GetRuntimeDiagnostics(ctx context.Context) (json.RawMessage, error) {
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.GetRuntimeDiagnostics(ctx)
}

func (c *RuntimeIrisClient) GetChatroomFields(ctx context.Context, chatID int64) (json.RawMessage, error) {
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.GetChatroomFields(ctx, chatID)
}

func (c *RuntimeIrisClient) OpenChatroom(ctx context.Context, chatID int64) (json.RawMessage, error) {
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.OpenChatroom(ctx, chatID)
}

func (c *RuntimeIrisClient) GetTextPingDiagnostics(ctx context.Context, chatID int64) (json.RawMessage, error) {
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.GetTextPingDiagnostics(ctx, chatID)
}

func (c *RuntimeIrisClient) WarmTextPing(ctx context.Context, chatID int64) (*iris.TextPingWarmResponse, error) {
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.WarmTextPing(ctx, chatID)
}

func (c *RuntimeIrisClient) SendKaring(ctx context.Context, req iris.KaringSendRequest) (*iris.KaringDryRunResponse, error) {
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.SendKaring(ctx, req)
}

func (c *RuntimeIrisClient) SendKaringContentList(ctx context.Context, req iris.KaringContentListRequest) (*iris.KaringDryRunResponse, error) {
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.SendKaringContentList(ctx, req)
}

func (c *RuntimeIrisClient) SendKaringHololive(ctx context.Context, req iris.KaringHololiveRequest) (*iris.KaringDryRunResponse, error) {
	client, err := c.currentClient()
	if err != nil {
		return nil, err
	}
	return client.SendKaringHololive(ctx, req)
}

func (c *RuntimeIrisClient) currentClient() (*iris.H2CClient, error) {
	if c == nil {
		return nil, fmt.Errorf("runtime iris client: client is nil")
	}
	if c.botToken == "" {
		return nil, fmt.Errorf("runtime iris client: bot token is empty")
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("runtime iris client: client is closed")
	}
	baseURL, err := c.resolveBaseURLLocked()
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}

	if c.cachedH2CClient != nil && c.cachedBaseURL == baseURL {
		client := c.cachedH2CClient
		c.mu.Unlock()
		return client, nil
	}

	next := iris.NewH2CClient(baseURL, c.botToken, c.clientOpts...)
	if err := next.InitError(); err != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("runtime iris client: initialize %s: %w", baseURL, err)
	}

	previous := c.cachedH2CClient
	c.cachedBaseURL = baseURL
	c.cachedH2CClient = next
	c.scheduleStaleCloseLocked(previous)
	c.mu.Unlock()

	return next, nil
}

func (c *RuntimeIrisClient) Close() error {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	client := c.cachedH2CClient
	c.cachedH2CClient = nil
	c.cachedBaseURL = ""
	c.closed = true
	close(c.closeSignal)
	c.mu.Unlock()

	c.staleClosers.Wait()

	if client == nil {
		return nil
	}

	return client.Close()
}

// scheduleStaleCloseLocked는 base URL 회전으로 교체된 이전 client를 grace 기간 뒤에
// 닫아, 회전 순간 해당 client로 진행 중이던 요청(특히 active conn을 끊는 h3)이 끝날 시간을
// 준다. RuntimeIrisClient.Close()는 closeSignal로 대기 중인 stale close를 즉시 깨운다.
// mu를 잡은 상태에서 호출해야 하며(WaitGroup Add가 Close의 Wait보다 happens-before),
// 실제 teardown은 goroutine에서 lock 밖으로 수행한다.
func (c *RuntimeIrisClient) scheduleStaleCloseLocked(client *iris.H2CClient) {
	if client == nil {
		return
	}

	c.staleClosers.Add(1)
	go c.runStaleClose(client, c.staleCloseGrace)
}

func (c *RuntimeIrisClient) runStaleClose(client *iris.H2CClient, grace time.Duration) {
	defer c.staleClosers.Done()

	if grace > 0 {
		c.awaitStaleCloseGrace(grace)
	}
	c.closeStaleClient(client)
}

func (c *RuntimeIrisClient) awaitStaleCloseGrace(grace time.Duration) {
	timer := time.NewTimer(grace)
	defer timer.Stop()

	select {
	case <-timer.C:
	case <-c.closeSignal:
	}
}

func (c *RuntimeIrisClient) closeStaleClient(client *iris.H2CClient) {
	if err := client.Close(); err != nil && c.logger != nil {
		c.logger.Warn("runtime_iris_client_close_stale_failed", slog.Any("error", err))
	}
}

func (c *RuntimeIrisClient) ValidateBaseURL() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, err := c.resolveBaseURLLocked()
	if err != nil {
		return err
	}
	return nil
}

func (c *RuntimeIrisClient) resolveBaseURLLocked() (string, error) {
	if c == nil {
		return "", fmt.Errorf("runtime iris client: client is nil")
	}

	if c.baseURLFilePath != "" {
		return c.resolveBaseURLFromFileLocked()
	}

	return validateHTTPBaseURL(c.fallbackBaseURL)
}

func (c *RuntimeIrisClient) resolveBaseURLFromFileLocked() (string, error) {
	validateStat := shouldValidateRuntimeIrisBaseURLFileStat()
	baseURLFilePath, err := normalizeRuntimeIrisBaseURLFilePath(c.baseURLFilePath, validateStat)
	if err != nil {
		return "", fmt.Errorf("validate IRIS_BASE_URL_FILE path: %w", err)
	}

	if validateStat {
		if err := validateRuntimeIrisBaseURLFileStat(baseURLFilePath); err != nil {
			return "", fmt.Errorf("validate IRIS_BASE_URL_FILE: %w", err)
		}
	}

	raw, err := os.ReadFile(baseURLFilePath)
	if err != nil {
		return "", fmt.Errorf("read IRIS_BASE_URL_FILE: %w", err)
	}

	baseURL, err := validateRuntimeIrisBaseURLFileOverride(string(raw), c.warnBaseURLHostUnvalidated)
	if err != nil {
		return "", fmt.Errorf("validate IRIS_BASE_URL_FILE URL: %w", err)
	}

	return baseURL, nil
}

func (c *RuntimeIrisClient) warnBaseURLHostUnvalidated(host string) {
	if c == nil || c.logger == nil {
		return
	}

	c.baseURLHostUnvalidatedWarnOnce.Do(func() {
		c.logger.Warn("IRIS_BASE_URL_FILE host is unvalidated because no Iris base URL allowlist is configured",
			slog.String("host", host),
			slog.String("path", c.baseURLFilePath),
			slog.String("allowlist_env", irisH3ServerNameEnv+","+irisBaseURLAllowedHostsEnv),
		)
	})
}

func validateHTTPBaseURL(raw string) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(raw), "/")
	if baseURL == "" {
		return "", fmt.Errorf("base URL is empty")
	}

	parsed, err := url.ParseRequestURI(baseURL)
	if err != nil {
		return "", err
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme: %q", parsed.Scheme)
	}

	if parsed.Host == "" {
		return "", fmt.Errorf("base URL host is empty")
	}

	return baseURL, nil
}
