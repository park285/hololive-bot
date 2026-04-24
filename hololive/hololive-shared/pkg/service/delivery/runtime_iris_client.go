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

	baseURL, err := c.resolveBaseURL()
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cachedH2CClient != nil && c.cachedBaseURL == baseURL {
		return c.cachedH2CClient, nil
	}

	c.cachedBaseURL = baseURL
	c.cachedH2CClient = iris.NewH2CClient(baseURL, c.botToken, c.clientOpts...)
	return c.cachedH2CClient, nil
}

func (c *RuntimeIrisClient) resolveBaseURL() (string, error) {
	if c == nil {
		return "", fmt.Errorf("runtime iris client: client is nil")
	}

	if c.baseURLFilePath != "" {
		raw, err := os.ReadFile(c.baseURLFilePath)
		switch {
		case err == nil:
			baseURL := strings.TrimSpace(string(raw))
			if baseURL == "" {
				c.logBaseURLFileFallback("base URL file is empty")
				break
			}
			if _, parseErr := url.ParseRequestURI(baseURL); parseErr != nil {
				c.logBaseURLFileFallback(fmt.Sprintf("parse base URL file: %v", parseErr))
				break
			}
			return strings.TrimRight(baseURL, "/"), nil
		case !os.IsNotExist(err):
			c.logBaseURLFileFallback(fmt.Sprintf("read base URL file: %v", err))
		}
	}

	if c.fallbackBaseURL == "" {
		return "", fmt.Errorf("runtime iris client: fallback base URL is empty")
	}
	if _, err := url.ParseRequestURI(c.fallbackBaseURL); err != nil {
		return "", fmt.Errorf("runtime iris client: parse fallback base URL: %w", err)
	}
	return strings.TrimRight(c.fallbackBaseURL, "/"), nil
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
