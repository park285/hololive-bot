package delivery

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/park285/iris-client-go/iris"
)

type RuntimeIrisClient struct {
	fallbackBaseURL string
	botToken        string
	baseURLFilePath string
	logger          *slog.Logger
	clientOpts      []iris.ClientOption

	mu              sync.Mutex
	cachedBaseURL   string
	cachedH2CClient *iris.H2CClient
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

	clientOpts := make([]iris.ClientOption, 0, len(opts)+1)
	clientOpts = append(clientOpts, iris.WithLogger(logger))
	clientOpts = append(clientOpts, opts...)

	return &RuntimeIrisClient{
		fallbackBaseURL: strings.TrimSpace(fallbackBaseURL),
		botToken:        strings.TrimSpace(botToken),
		baseURLFilePath: strings.TrimSpace(baseURLFilePath),
		logger:          logger,
		clientOpts:      clientOpts,
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
	defer c.mu.Unlock()

	baseURL, err := c.resolveBaseURLLocked()
	if err != nil {
		return nil, err
	}

	if c.cachedH2CClient != nil && c.cachedBaseURL == baseURL {
		return c.cachedH2CClient, nil
	}

	c.cachedBaseURL = baseURL
	c.cachedH2CClient = iris.NewH2CClient(baseURL, c.botToken, c.clientOpts...)
	return c.cachedH2CClient, nil
}

func (c *RuntimeIrisClient) resolveBaseURLLocked() (string, error) {
	if c == nil {
		return "", fmt.Errorf("runtime iris client: client is nil")
	}

	if c.baseURLFilePath != "" {
		fileBaseURL, err := c.resolveBaseURLFromFileLocked()
		if err == nil && fileBaseURL != "" {
			return fileBaseURL, nil
		}
		if err != nil {
			c.logBaseURLFileFallback(err.Error())
		}
	}

	return validateHTTPBaseURL(c.fallbackBaseURL)
}

func (c *RuntimeIrisClient) resolveBaseURLFromFileLocked() (string, error) {
	raw, err := os.ReadFile(c.baseURLFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read base URL file: %w", err)
	}

	baseURL, err := validateRuntimeIrisBaseURL(string(raw))
	if err != nil {
		return "", fmt.Errorf("parse base URL file: %w", err)
	}

	return baseURL, nil
}

func validateRuntimeIrisBaseURL(raw string) (string, error) {
	baseURL, err := validateHTTPBaseURL(raw)
	if err != nil {
		return "", err
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}

	if err := validateRuntimeIrisTransportScheme(normalizeRuntimeIrisTransport(os.Getenv("IRIS_TRANSPORT")), parsed.Scheme); err != nil {
		return "", err
	}

	return baseURL, nil
}

func validateRuntimeIrisTransportScheme(transport, scheme string) error {
	requiredScheme, ok := runtimeIrisTransportRequiredSchemes()[transport]
	if !ok || scheme == requiredScheme {
		return nil
	}
	return fmt.Errorf("IRIS_TRANSPORT=%s requires %s IRIS_BASE_URL, got %s", transport, requiredScheme, scheme)
}

func runtimeIrisTransportRequiredSchemes() map[string]string {
	return map[string]string{
		"h3":    "https",
		"h2c":   "http",
		"http2": "https",
	}
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

func normalizeRuntimeIrisTransport(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if isRuntimeIrisHTTP3Transport(normalized) {
		return "h3"
	}
	if normalized == "h2c" {
		return normalized
	}
	if normalized == "h2" || normalized == "http2" {
		return "http2"
	}
	if normalized == "http1" || normalized == "http" || normalized == "http/1.1" {
		return "http1"
	}
	return normalized
}

func isRuntimeIrisHTTP3Transport(normalized string) bool {
	return normalized == "h3" || normalized == "http3" || normalized == "http/3" || normalized == "quic"
}

func (c *RuntimeIrisClient) logBaseURLFileFallback(reason string) {
	if c == nil || c.logger == nil || strings.TrimSpace(reason) == "" {
		return
	}

	c.logger.Warn("Runtime Iris client falling back to configured base URL",
		slog.String("path", c.baseURLFilePath),
		slog.String("reason", reason),
	)
}
