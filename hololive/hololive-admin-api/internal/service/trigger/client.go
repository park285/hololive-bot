// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package trigger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
)

// Client는 llm-scheduler 내부 트리거 API를 호출한다.
type Client struct {
	httpClient *httputil.JSONClient
	logger     *slog.Logger
}

// NewClient는 trigger 클라이언트를 생성한다.
func NewClient(schedulerURL, apiKey string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}

	return &Client{
		httpClient: httputil.NewJSONClient(schedulerURL, apiKey, 30*time.Second),
		logger:     logger,
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
		return errors.New("post trigger: client is nil")
	}

	req, err := c.httpClient.NewRequest(ctx, http.MethodPost, path)
	if err != nil {
		return fmt.Errorf("post trigger: new request %s: %w", path, err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post trigger: request %s: %w", path, err)
	}

	if resp.StatusCode == http.StatusConflict {
		_ = resp.Body.Close()

		c.logger.Info("Trigger notification already in progress", slog.String("path", path))

		return triggercontracts.ErrNotificationInProgress
	}

	if err := c.httpClient.CheckStatus(resp); err != nil {
		_ = resp.Body.Close()

		return fmt.Errorf(
			"post trigger: request failed %s: %w",
			path,
			err,
		)
	}

	if err := c.httpClient.DiscardBody(resp); err != nil {
		return fmt.Errorf("post trigger: discard response body %s: %w", path, err)
	}

	c.logger.Debug("Trigger request succeeded", slog.String("path", path), slog.Int("status", resp.StatusCode))

	return nil
}
