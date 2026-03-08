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
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/domain"
)

var errBlockedLink = errors.New("parse link: blocked host")

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

// NewLinkChecker는 LinkChecker를 생성한다.
func NewLinkChecker(client *http.Client, cfg LinkCheckerConfig, logger *slog.Logger) *LinkChecker {
	if client == nil {
		client = httputil.NewExternalAPIClient(defaultLinkCheckerHTTPClient)
	}
	if logger == nil {
		logger = slog.Default()
	}

	normalized := cfg
	defaults := DefaultLinkCheckerConfig()
	if normalized.Timeout <= 0 {
		normalized.Timeout = defaults.Timeout
	}
	if normalized.Concurrency < 1 {
		normalized.Concurrency = defaults.Concurrency
	}

	resolver := net.DefaultResolver
	client = withBlockedRedirectPolicy(client, resolver, normalized.Timeout)

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
		event := event
		if event == nil {
			continue
		}

		eg.Go(func() error {
			status, checkErr := c.CheckLink(egCtx, event.Link)
			checkedAt := time.Now().UTC()

			mu.Lock()
			event.LinkStatus = status
			event.LinkCheckedAt = &checkedAt
			result.Checked++
			switch status {
			case domain.MajorEventLinkStatusOK:
				result.OK++
			case domain.MajorEventLinkStatusBlocked:
				result.Blocked++
			default:
				result.Failed++
			}
			mu.Unlock()

			if checkErr != nil {
				c.logger.Debug(
					"Major event link check failed",
					slog.String("link", redactLinkForLog(event.Link)),
					slog.String("status", string(status)),
					slog.String("error", checkErr.Error()),
				)
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return result, fmt.Errorf("check links: wait workers: %w", err)
	}

	return result, nil
}

// CheckLink는 단일 링크를 검증한다.
func (c *LinkChecker) CheckLink(ctx context.Context, rawURL string) (domain.MajorEventLinkStatus, error) {
	parsed, err := parseAndValidateLink(rawURL)
	if err != nil {
		if isBlockedLinkError(err) {
			return domain.MajorEventLinkStatusBlocked, err
		}
		return domain.MajorEventLinkStatusFailed, err
	}

	headStatus, headErr := c.probe(ctx, http.MethodHead, parsed.String())
	if isBlockedLinkError(headErr) {
		return domain.MajorEventLinkStatusBlocked, headErr
	}
	if headErr == nil {
		if isSuccessStatus(headStatus) {
			return domain.MajorEventLinkStatusOK, nil
		}
		if !shouldFallbackToGET(headStatus, nil) {
			return domain.MajorEventLinkStatusFailed, fmt.Errorf("check link: head status %d", headStatus)
		}
	} else if !shouldFallbackToGET(0, headErr) {
		return domain.MajorEventLinkStatusFailed, fmt.Errorf("check link: head request failed: %w", headErr)
	}

	getStatus, getErr := c.probe(ctx, http.MethodGet, parsed.String())
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

func (c *LinkChecker) validateRequestTarget(ctx context.Context, rawURL string) error {
	parsed, err := parseAndValidateLink(rawURL)
	if err != nil {
		return err
	}
	return validateResolvedHost(ctx, c.resolver, c.config.Timeout, parsed)
}

func parseAndValidateLink(rawURL string) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, fmt.Errorf("parse link: empty url")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("parse link: %w", err)
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("parse link: unsupported scheme %q", parsed.Scheme)
	}
	hostname := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if hostname == "" {
		return nil, fmt.Errorf("parse link: empty host")
	}
	if hostname == "localhost" {
		return nil, fmt.Errorf("%w %q", errBlockedLink, parsed.Host)
	}
	if ip := net.ParseIP(hostname); ip != nil && isPrivateOrInternalIP(ip) {
		return nil, fmt.Errorf("%w %q", errBlockedLink, parsed.Host)
	}
	return parsed, nil
}

func validateResolvedHost(ctx context.Context, resolver hostResolver, timeout time.Duration, parsed *url.URL) error {
	if resolver == nil || parsed == nil {
		return nil
	}

	lookupCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		lookupCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	ips, err := resolver.LookupIP(lookupCtx, "ip", parsed.Hostname())
	if err != nil {
		return fmt.Errorf("resolve link host: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("resolve link host: no addresses for %q", parsed.Hostname())
	}
	for _, ip := range ips {
		if isPrivateOrInternalIP(ip) {
			return fmt.Errorf("%w %q", errBlockedLink, parsed.Host)
		}
	}
	return nil
}

func isBlockedLinkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errBlockedLink) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "unsupported scheme")
}

func withBlockedRedirectPolicy(client *http.Client, resolver hostResolver, timeout time.Duration) *http.Client {
	if client == nil {
		return nil
	}

	cloned := *client
	original := client.CheckRedirect
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := validateResolvedHost(req.Context(), resolver, timeout, req.URL); err != nil {
			return err
		}
		if original != nil {
			return original(req, via)
		}
		return nil
	}
	return &cloned
}

func isPrivateOrInternalIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
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

func redactLinkForLog(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
