package trigger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

const (
	defaultTriggerHTTPTimeout = 30 * time.Second
)

// Client는 llm-scheduler 내부 트리거 API를 호출한다.
type Client struct {
	schedulerURL string
	apiKey       string
	httpClient   *http.Client
	logger       *slog.Logger
}

// NewClient는 trigger 클라이언트를 생성한다.
func NewClient(schedulerURL, apiKey string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		schedulerURL: strings.TrimRight(strings.TrimSpace(schedulerURL), "/"),
		apiKey:       strings.TrimSpace(apiKey),
		httpClient: &http.Client{
			Timeout: defaultTriggerHTTPTimeout,
		},
		logger: logger,
	}
}

// SendWeeklyNotification은 주간 major event 알림 트리거를 호출한다.
func (c *Client) SendWeeklyNotification(ctx context.Context) error {
	if err := c.postTrigger(ctx, triggercontracts.MajorEventWeeklyPath); err != nil {
		return fmt.Errorf("send weekly notification: %w", err)
	}
	return nil
}

// SendMonthlyNotification은 월간 major event 알림 트리거를 호출한다.
func (c *Client) SendMonthlyNotification(ctx context.Context) error {
	if err := c.postTrigger(ctx, triggercontracts.MajorEventMonthlyPath); err != nil {
		return fmt.Errorf("send monthly notification: %w", err)
	}
	return nil
}

// SendMemberNewsWeekly는 주간 member news 알림 트리거를 호출한다.
func (c *Client) SendMemberNewsWeekly(ctx context.Context) error {
	if err := c.postTrigger(ctx, triggercontracts.MemberNewsWeeklyPath); err != nil {
		return fmt.Errorf("send member news weekly notification: %w", err)
	}
	return nil
}

func (c *Client) postTrigger(ctx context.Context, path string) error {
	if c == nil {
		return fmt.Errorf("post trigger: client is nil")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.schedulerURL+path, http.NoBody)
	if err != nil {
		return fmt.Errorf("post trigger: build request %s: %w", path, err)
	}
	if c.apiKey != "" {
		req.Header.Set(sharedserver.APIKeyHeader, c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post trigger: execute request %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusConflict {
		c.logger.Info("Trigger notification already in progress", slog.String("path", path))
		return triggercontracts.ErrNotificationInProgress
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		const maxBodyLen = 4096
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyLen))
		return fmt.Errorf(
			"post trigger: request failed %s: status %d: %s",
			path,
			resp.StatusCode,
			strings.TrimSpace(string(body)),
		)
	}

	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return fmt.Errorf("post trigger: discard response body %s: %w", path, err)
	}

	c.logger.Debug("Trigger request succeeded", slog.String("path", path), slog.Int("status", resp.StatusCode))
	return nil
}
