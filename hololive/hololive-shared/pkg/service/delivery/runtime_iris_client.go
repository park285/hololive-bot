package delivery

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

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

	baseURLFileCachedValid   bool
	baseURLFileCachedModTime time.Time
	baseURLFileCachedSize    int64
	baseURLFileCachedValue   string
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
	stat, err := os.Stat(c.baseURLFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}

		c.baseURLFileCachedValid = false
		return "", fmt.Errorf("stat base URL file: %w", err)
	}

	if c.baseURLFileCachedValid &&
		stat.ModTime().Equal(c.baseURLFileCachedModTime) &&
		stat.Size() == c.baseURLFileCachedSize {
		return c.baseURLFileCachedValue, nil
	}

	raw, err := os.ReadFile(c.baseURLFilePath)
	if err != nil {
		c.baseURLFileCachedValid = false
		return "", fmt.Errorf("read base URL file: %w", err)
	}

	baseURL, err := validateHTTPBaseURL(string(raw))
	if err != nil {
		c.baseURLFileCachedValid = false
		return "", fmt.Errorf("parse base URL file: %w", err)
	}

	c.baseURLFileCachedValid = true
	c.baseURLFileCachedModTime = stat.ModTime()
	c.baseURLFileCachedSize = stat.Size()
	c.baseURLFileCachedValue = baseURL

	return baseURL, nil
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

func (c *RuntimeIrisClient) logBaseURLFileFallback(reason string) {
	if c == nil || c.logger == nil || strings.TrimSpace(reason) == "" {
		return
	}

	c.logger.Warn("Runtime Iris client falling back to configured base URL",
		slog.String("path", c.baseURLFilePath),
		slog.String("reason", reason),
	)
}
