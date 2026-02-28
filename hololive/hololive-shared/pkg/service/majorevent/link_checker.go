package majorevent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type linkCheckRepository interface {
	ListEventsNeedingLinkCheck(ctx context.Context, staleBefore time.Time, limit int) ([]*domain.MajorEvent, error)
	UpdateEventLinkStatus(ctx context.Context, eventID int, status domain.MajorEventLinkStatus, checkedAt time.Time) error
}

type hostResolver func(ctx context.Context, host string) ([]net.IP, error)

type LinkChecker struct {
	httpClient     HTTPClient
	repository     linkCheckRepository
	logger         *slog.Logger
	now            func() time.Time
	resolveHostIPs hostResolver
	requestTimeout time.Duration
	staleAfter     time.Duration
	batchSize      int
	userAgent      string
}

type LinkCheckResult struct {
	Checked int
	OK      int
	Failed  int
	Blocked int
}

const maxRedirectHops = 10

func NewLinkChecker(httpClient HTTPClient, repository linkCheckRepository, logger *slog.Logger) *LinkChecker {
	if logger == nil {
		logger = slog.Default()
	}
	return &LinkChecker{
		httpClient:     httpClient,
		repository:     repository,
		logger:         logger,
		now:            time.Now,
		resolveHostIPs: defaultHostResolver,
		requestTimeout: constants.MajorEventConfig.LinkCheckTimeout,
		staleAfter:     constants.MajorEventConfig.LinkCheckStaleAfter,
		batchSize:      constants.MajorEventConfig.LinkCheckBatchSize,
		userAgent:      constants.MajorEventConfig.UserAgent,
	}
}

func (c *LinkChecker) CheckStaleLinks(ctx context.Context) (LinkCheckResult, error) {
	var result LinkCheckResult
	if c == nil {
		return result, fmt.Errorf("link checker is nil")
	}
	if c.repository == nil {
		return result, fmt.Errorf("link checker repository is nil")
	}
	if c.httpClient == nil {
		return result, fmt.Errorf("link checker http client is nil")
	}

	staleAfter := c.staleAfter
	if staleAfter <= 0 {
		staleAfter = 72 * time.Hour
	}
	batchSize := c.batchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	staleBefore := c.now().UTC().Add(-staleAfter)
	events, err := c.repository.ListEventsNeedingLinkCheck(ctx, staleBefore, batchSize)
	if err != nil {
		return result, fmt.Errorf("list events needing link check: %w", err)
	}
	if len(events) == 0 {
		c.logger.Debug("Major event link check skipped: no stale targets",
			slog.Time("stale_before", staleBefore),
			slog.Int("batch_size", batchSize))
		return result, nil
	}

	var updateErr error
	for i := range events {
		event := events[i]
		if event == nil {
			continue
		}

		status, checkErr := c.checkLink(ctx, event.Link)
		now := c.now().UTC()
		if err := c.repository.UpdateEventLinkStatus(ctx, event.ID, status, now); err != nil {
			updateErr = errors.Join(updateErr, fmt.Errorf("event id=%d update link status: %w", event.ID, err))
			continue
		}

		result.Checked++
		switch status {
		case domain.MajorEventLinkStatusOK:
			result.OK++
		case domain.MajorEventLinkStatusBlocked:
			result.Blocked++
		default:
			result.Failed++
		}

		if checkErr != nil {
			c.logger.Debug("Major event link check failed",
				slog.Int("event_id", event.ID),
				slog.String("status", string(status)),
				slog.String("link", event.Link),
				slog.String("error", checkErr.Error()),
			)
		}
	}

	if updateErr != nil {
		return result, updateErr
	}

	return result, nil
}

func (c *LinkChecker) checkLink(ctx context.Context, rawURL string) (domain.MajorEventLinkStatus, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return domain.MajorEventLinkStatusFailed, fmt.Errorf("link is empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return domain.MajorEventLinkStatusFailed, fmt.Errorf("parse link: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return domain.MajorEventLinkStatusBlocked, fmt.Errorf("unsupported link scheme: %s", parsed.Scheme)
	}

	if err := c.validateHost(ctx, parsed.Hostname()); err != nil {
		var blockErr *linkBlockError
		if errors.As(err, &blockErr) {
			return domain.MajorEventLinkStatusBlocked, err
		}
		return domain.MajorEventLinkStatusFailed, err
	}

	headStatus, headErr := c.probeURL(ctx, parsed.String(), http.MethodHead)
	if headErr == nil && isSuccessStatus(headStatus) {
		return domain.MajorEventLinkStatusOK, nil
	}
	if isBlockedLinkError(headErr) {
		return domain.MajorEventLinkStatusBlocked, fmt.Errorf("head request blocked: %w", headErr)
	}

	if !shouldFallbackToGet(headStatus, headErr) {
		if headErr != nil {
			return domain.MajorEventLinkStatusFailed, fmt.Errorf("head request failed: %w", headErr)
		}
		return domain.MajorEventLinkStatusFailed, fmt.Errorf("head status code: %d", headStatus)
	}

	getStatus, getErr := c.probeURL(ctx, parsed.String(), http.MethodGet)
	if getErr == nil && isSuccessStatus(getStatus) {
		return domain.MajorEventLinkStatusOK, nil
	}
	if isBlockedLinkError(getErr) {
		return domain.MajorEventLinkStatusBlocked, fmt.Errorf("get request blocked: %w", getErr)
	}
	if getErr != nil {
		if headErr != nil {
			return domain.MajorEventLinkStatusFailed, fmt.Errorf("head/get failed: %w", errors.Join(headErr, getErr))
		}
		return domain.MajorEventLinkStatusFailed, fmt.Errorf("get request failed: %w", getErr)
	}
	return domain.MajorEventLinkStatusFailed, fmt.Errorf("get status code: %d", getStatus)
}

func (c *LinkChecker) probeURL(ctx context.Context, targetURL, method string) (int, error) {
	timeout := c.requestTimeout
	if timeout <= 0 {
		timeout = 8 * time.Second
	}

	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, method, targetURL, http.NoBody)
	if err != nil {
		return 0, fmt.Errorf("create %s request: %w", method, err)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if method == http.MethodGet {
		req.Header.Set("Range", "bytes=0-0")
	}

	resp, err := c.doRequest(req)
	if err != nil {
		return 0, fmt.Errorf("link check request: %w", err)
	}
	defer func() {
		_, _ = io.CopyN(io.Discard, resp.Body, 1024)
		_ = resp.Body.Close()
	}()

	return resp.StatusCode, nil
}

func (c *LinkChecker) doRequest(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if baseClient, ok := c.httpClient.(*http.Client); ok && baseClient != nil {
		return c.doRequestWithRedirectValidation(baseClient, req)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	return resp, nil
}

func (c *LinkChecker) doRequestWithRedirectValidation(baseClient *http.Client, req *http.Request) (*http.Response, error) {
	if baseClient == nil {
		return nil, fmt.Errorf("base http client is nil")
	}
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}

	guardedClient := *baseClient
	prevRedirectCheck := baseClient.CheckRedirect
	guardedClient.CheckRedirect = func(nextReq *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirectHops {
			return fmt.Errorf("stopped after %d redirects", maxRedirectHops)
		}
		if err := c.validateHost(nextReq.Context(), nextReq.URL.Hostname()); err != nil {
			return fmt.Errorf("redirect host validation: %w", err)
		}
		if prevRedirectCheck != nil {
			if err := prevRedirectCheck(nextReq, via); err != nil {
				return err
			}
		}
		return nil
	}

	resp, err := guardedClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do guarded request: %w", err)
	}
	return resp, nil
}

func shouldFallbackToGet(statusCode int, reqErr error) bool {
	if reqErr != nil {
		return true
	}

	switch statusCode {
	case http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusMethodNotAllowed,
		http.StatusNotAcceptable,
		http.StatusTooManyRequests,
		http.StatusNotImplemented,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func isSuccessStatus(code int) bool {
	return code >= 200 && code < 400
}

func isBlockedLinkError(err error) bool {
	if err == nil {
		return false
	}
	var blockErr *linkBlockError
	return errors.As(err, &blockErr)
}

func (c *LinkChecker) validateHost(ctx context.Context, host string) error {
	normalizedHost := strings.ToLower(strings.TrimSpace(host))
	if normalizedHost == "" {
		return fmt.Errorf("link host is empty")
	}

	if isBlockedHostname(normalizedHost) {
		return &linkBlockError{reason: fmt.Sprintf("blocked hostname: %s", normalizedHost)}
	}

	if ip := net.ParseIP(normalizedHost); ip != nil {
		if isPrivateOrLocalIP(ip) {
			return &linkBlockError{reason: fmt.Sprintf("blocked ip address: %s", normalizedHost)}
		}
		return nil
	}

	ips, err := c.resolveHostIPs(ctx, normalizedHost)
	if err != nil {
		return fmt.Errorf("resolve host %s: %w", normalizedHost, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("host %s resolved to no ip", normalizedHost)
	}

	for _, ip := range ips {
		if isPrivateOrLocalIP(ip) {
			return &linkBlockError{reason: fmt.Sprintf("blocked resolved ip %s for host %s", ip.String(), normalizedHost)}
		}
	}

	return nil
}

func defaultHostResolver(ctx context.Context, host string) ([]net.IP, error) {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve host %s: %w", host, err)
	}
	resolved := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		resolved = append(resolved, ip.IP)
	}
	return resolved, nil
}

func isBlockedHostname(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	if strings.HasSuffix(host, ".local") {
		return true
	}
	return false
}

func isPrivateOrLocalIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalMulticast() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	// Carrier-grade NAT: 100.64.0.0/10
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 100 && ip4[1]&0xC0 == 0x40 {
		return true
	}
	return false
}

type linkBlockError struct {
	reason string
}

func (e *linkBlockError) Error() string {
	return e.reason
}
