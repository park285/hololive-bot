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

package scraper

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/park285/shared-go/pkg/httputil"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// LinkChecker는 링크 유효성 검증(HEAD 후 GET fallback)을 수행한다.
type LinkChecker struct {
	client   *http.Client
	config   LinkCheckerConfig
	logger   *slog.Logger
	resolver hostResolver
}

type hostResolver interface {
	LookupIP(context.Context, string, string) ([]net.IP, error)
}

type eventLinkCheck struct {
	event     *domain.MajorEvent
	status    domain.MajorEventLinkStatus
	checkedAt time.Time
	err       error
}

// NewLinkChecker는 LinkChecker를 생성한다.
func NewLinkChecker(client *http.Client, config LinkCheckerConfig, logger *slog.Logger) *LinkChecker {
	if client == nil {
		client = httputil.NewExternalAPIClient(defaultLinkCheckerHTTPClient)
	}
	if logger == nil {
		logger = slog.Default()
	}

	normalized := config
	defaults := DefaultLinkCheckerConfig()
	if normalized.Timeout <= 0 {
		normalized.Timeout = defaults.Timeout
	}
	if normalized.Concurrency < 1 {
		normalized.Concurrency = defaults.Concurrency
	}

	resolver := net.DefaultResolver
	client = withBlockedRedirectPolicy(client, resolver, normalized.Timeout)
	client = withValidatedDialPolicy(client, resolver, normalized.Timeout)

	return &LinkChecker{
		client:   client,
		config:   normalized,
		logger:   logger,
		resolver: resolver,
	}
}

// CheckEvents는 이벤트 링크를 병렬 검증한다.
func (c *LinkChecker) CheckEvents(ctx context.Context, events []*domain.MajorEvent) (LinkCheckResult, error) {
	result := LinkCheckResult{}
	if len(events) == 0 {
		return result, nil
	}

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(c.config.Concurrency)

	var mu sync.Mutex

	for _, event := range events {
		if event == nil {
			continue
		}

		eg.Go(func() error {
			check := c.checkEventLink(egCtx, event)

			mu.Lock()
			applyEventLinkCheck(&result, check)
			mu.Unlock()

			c.logEventLinkCheckFailure(check)
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return result, fmt.Errorf("check links: wait workers: %w", err)
	}

	return result, nil
}

func (c *LinkChecker) checkEventLink(ctx context.Context, event *domain.MajorEvent) eventLinkCheck {
	status, err := c.CheckLink(ctx, event.Link)
	return eventLinkCheck{
		event:     event,
		status:    status,
		checkedAt: time.Now().UTC(),
		err:       err,
	}
}

func applyEventLinkCheck(result *LinkCheckResult, check eventLinkCheck) {
	check.event.LinkStatus = check.status
	check.event.LinkCheckedAt = &check.checkedAt
	result.Checked++
	addLinkCheckStatus(result, check.status)
}

func addLinkCheckStatus(result *LinkCheckResult, status domain.MajorEventLinkStatus) {
	switch status {
	case domain.MajorEventLinkStatusOK:
		result.OK++
	case domain.MajorEventLinkStatusBlocked:
		result.Blocked++
	default:
		result.Failed++
	}
}

func (c *LinkChecker) logEventLinkCheckFailure(check eventLinkCheck) {
	if check.err == nil {
		return
	}
	c.logger.Debug(
		"Major event link check failed",
		slog.String("link", redactLinkForLog(check.event.Link)),
		slog.String("status", string(check.status)),
		slog.String("error", check.err.Error()),
	)
}

// CheckLink는 단일 링크를 검증한다.
func (c *LinkChecker) CheckLink(ctx context.Context, rawURL string) (domain.MajorEventLinkStatus, error) {
	parsed, err := parseAndValidateLink(rawURL)
	if err != nil {
		return statusForLinkError(err), err
	}

	targetURL := parsed.String()
	if status, done, err := c.checkHeadLink(ctx, targetURL); done {
		return status, err
	}
	return c.checkGetLink(ctx, targetURL)
}

func statusForLinkError(err error) domain.MajorEventLinkStatus {
	if isBlockedLinkError(err) {
		return domain.MajorEventLinkStatusBlocked
	}
	return domain.MajorEventLinkStatusFailed
}

func (c *LinkChecker) checkHeadLink(ctx context.Context, targetURL string) (domain.MajorEventLinkStatus, bool, error) {
	headStatus, headErr := c.probe(ctx, http.MethodHead, targetURL)
	if isBlockedLinkError(headErr) {
		return domain.MajorEventLinkStatusBlocked, true, headErr
	}
	if headErr == nil {
		return statusForHeadResponse(headStatus)
	}
	if shouldFallbackToGET(0, headErr) {
		return "", false, nil
	}
	return domain.MajorEventLinkStatusFailed, true, fmt.Errorf("check link: head request failed: %w", headErr)
}

func statusForHeadResponse(headStatus int) (domain.MajorEventLinkStatus, bool, error) {
	if isSuccessStatus(headStatus) {
		return domain.MajorEventLinkStatusOK, true, nil
	}
	if shouldFallbackToGET(headStatus, nil) {
		return "", false, nil
	}
	return domain.MajorEventLinkStatusFailed, true, fmt.Errorf("check link: head status %d", headStatus)
}

func (c *LinkChecker) checkGetLink(ctx context.Context, targetURL string) (domain.MajorEventLinkStatus, error) {
	getStatus, getErr := c.probe(ctx, http.MethodGet, targetURL)
	if isBlockedLinkError(getErr) {
		return domain.MajorEventLinkStatusBlocked, getErr
	}
	if getErr != nil {
		return domain.MajorEventLinkStatusFailed, fmt.Errorf("check link: get request failed: %w", getErr)
	}
	if isSuccessStatus(getStatus) {
		return domain.MajorEventLinkStatusOK, nil
	}
	return domain.MajorEventLinkStatusFailed, fmt.Errorf("check link: get status %d", getStatus)
}

func (c *LinkChecker) probe(ctx context.Context, method, targetURL string) (int, error) {
	if err := c.validateRequestTarget(ctx, targetURL); err != nil {
		return 0, err
	}

	requestCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, method, targetURL, http.NoBody)
	if err != nil {
		return 0, fmt.Errorf("probe link: build request: %w", err)
	}
	if method == http.MethodGet {
		req.Header.Set("Range", "bytes=0-0")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("probe link: do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode, nil
}

func isSuccessStatus(code int) bool {
	return code >= http.StatusOK && code < http.StatusMultipleChoices
}

func shouldFallbackToGET(statusCode int, probeErr error) bool {
	if probeErr != nil {
		normalized := strings.ToLower(probeErr.Error())
		return strings.Contains(normalized, "timeout") ||
			strings.Contains(normalized, "connection reset") ||
			strings.Contains(normalized, "method not allowed")
	}

	switch statusCode {
	case http.StatusMethodNotAllowed, http.StatusForbidden, http.StatusNotFound, http.StatusNotImplemented:
		return true
	default:
		return false
	}
}
