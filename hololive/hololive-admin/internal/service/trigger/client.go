package trigger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/majorevent"
)

// Client: llm-scheduler 내부 트리거 API를 프록시하는 HTTP 클라이언트
type Client struct {
	schedulerURL string
	httpClient   *http.Client
	logger       *slog.Logger
}

// NewClient: trigger proxy 클라이언트를 생성합니다.
func NewClient(schedulerURL string, logger *slog.Logger) *Client {
	schedulerURL = strings.TrimRight(schedulerURL, "/")
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		schedulerURL: schedulerURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// SendWeeklyNotification: llm-scheduler의 /internal/trigger/majorevent-weekly 엔드포인트를 호출합니다.
func (c *Client) SendWeeklyNotification(ctx context.Context) error {
	return c.postTrigger(ctx, "/internal/trigger/majorevent-weekly")
}

// SendMonthlyNotification: llm-scheduler의 /internal/trigger/majorevent-monthly 엔드포인트를 호출합니다.
func (c *Client) SendMonthlyNotification(ctx context.Context) error {
	return c.postTrigger(ctx, "/internal/trigger/majorevent-monthly")
}

// SendMemberNewsWeekly: llm-scheduler의 /internal/trigger/membernews-weekly 엔드포인트를 호출합니다.
func (c *Client) SendMemberNewsWeekly(ctx context.Context) error {
	return c.postTrigger(ctx, "/internal/trigger/membernews-weekly")
}

// postTrigger: 지정된 경로에 POST 요청을 보내고 결과를 처리합니다.
func (c *Client) postTrigger(ctx context.Context, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.schedulerURL+path, http.NoBody)
	if err != nil {
		return fmt.Errorf("trigger proxy: new request %s: %w", path, err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("trigger proxy: %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusConflict {
		return majorevent.ErrNotificationInProgress
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		const maxBodyLen = 4096
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyLen))
		return fmt.Errorf("trigger proxy: %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
