package htmlscraper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"
	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const officialScheduleAPIPath = "/api/list/2"

type officialScheduleSourceError struct {
	reason     officialScheduleReason
	statusCode int
	err        error
}

func (e *officialScheduleSourceError) Error() string {
	if e == nil || e.err == nil {
		return ""
	}

	return e.err.Error()
}

func (e *officialScheduleSourceError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.err
}

func (s *Service) fetchAllStreams(ctx context.Context) ([]*domain.Stream, error) {
	if cached, ok := s.getOfficialPageCache(); ok {
		s.observeOfficialScheduleResult("hit", cached)

		return cached, nil
	}

	resultCh := s.officialGroup.DoChan(officialScheduleCacheKey, func() (any, error) {
		return s.loadOfficialScheduleOrigin(ctx)
	})

	out, err := s.waitOfficialScheduleResult(ctx, resultCh)
	if err != nil {
		return out, fmt.Errorf("wait official schedule result: %w", err)
	}

	return out, nil
}

func (s *Service) loadOfficialScheduleOrigin(ctx context.Context) ([]*domain.Stream, error) {
	if cached, ok := s.getOfficialPageCache(); ok {
		return cached, nil
	}

	originCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.officialSchedule.Timeout)
	defer cancel()

	streams, err := s.fetchOfficialScheduleAPI(originCtx)
	if err != nil {
		return nil, fmt.Errorf("fetch official schedule API: %w", err)
	}

	s.setOfficialPageCache(streams)

	return streams, nil
}

func (s *Service) waitOfficialScheduleResult(ctx context.Context, resultCh <-chan singleflight.Result) ([]*domain.Stream, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for official schedule API: %w", ctx.Err())
	case result := <-resultCh:
		out, err := s.resolveOfficialScheduleResult(result.Val, result.Err, result.Shared)
		if err != nil {
			return out, fmt.Errorf("resolve official schedule result: %w", err)
		}

		return out, nil
	}
}

func (s *Service) resolveOfficialScheduleResult(value any, err error, shared bool) ([]*domain.Stream, error) {
	if err != nil {
		reason := classifyOfficialScheduleReason(err, 0)
		observeOfficialScheduleFallback("official_schedule_page", "error", reason)

		return nil, fmt.Errorf("load official schedule API: %w", err)
	}

	streams, ok := value.([]*domain.Stream)
	if !ok {
		return nil, fmt.Errorf("invalid official schedule cache result: %T", value)
	}

	if shared {
		s.logger.Debug("Official schedule API request deduplicated",
			slog.String("key", officialScheduleCacheKey),
			slog.Int("streams", len(streams)))
	}

	s.observeOfficialScheduleResult("hit", streams)

	return cloneStreams(streams), nil
}

func (s *Service) observeOfficialScheduleResult(outcome string, streams []*domain.Stream) {
	observeOfficialScheduleFallback("official_schedule_page", outcome, classifyOfficialScheduleReason(nil, len(streams)))
}

func (s *Service) fetchOfficialScheduleAPI(ctx context.Context) ([]*domain.Stream, error) {
	started := time.Now()
	outcome := "error"
	reason := officialScheduleReasonUnknown

	defer func() {
		observeOfficialScheduleRequest(outcome, reason, time.Since(started))
	}()

	req, err := s.newOfficialScheduleAPIRequest(ctx)
	if err != nil {
		reason = classifyOfficialScheduleReason(err, 0)
		return nil, fmt.Errorf("official schedule API request: %w", err)
	}

	body, err := s.executeOfficialScheduleAPIRequest(req)
	if err != nil {
		reason = classifyOfficialScheduleReason(err, 0)
		s.logOfficialScheduleAPIError(err)

		return nil, fmt.Errorf("execute official schedule API request: %w", err)
	}

	streams, stats, err := s.decodeOfficialScheduleAPI(body)
	observeOfficialScheduleRows(stats)

	if err != nil {
		reason = classifyOfficialScheduleReason(err, 0)
		s.logOfficialScheduleAPIError(err)

		return nil, fmt.Errorf("decode official schedule API: %w", err)
	}

	outcome = "success"
	reason = classifyOfficialScheduleReason(nil, len(streams))

	markOfficialScheduleSuccess()

	return streams, nil
}

func (s *Service) newOfficialScheduleAPIRequest(ctx context.Context) (*http.Request, error) {
	baseURL, err := url.Parse(strings.TrimSpace(s.officialSchedule.BaseURL))
	if err != nil {
		sourceErr := newOfficialScheduleSourceError(officialScheduleReasonRequest, 0, fmt.Errorf("parse official schedule base URL: %w", err))

		return nil, fmt.Errorf("official schedule source error: %w", sourceErr)
	}

	if validateErr := validateOfficialScheduleAPIBaseURL(baseURL); validateErr != nil {
		sourceErr := newOfficialScheduleSourceError(officialScheduleReasonRequest, 0, validateErr)

		return nil, fmt.Errorf("official schedule source error: %w", sourceErr)
	}

	baseURL.Path = officialScheduleAPIPath
	baseURL.RawQuery = ""
	baseURL.Fragment = ""

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), http.NoBody)
	if err != nil {
		sourceErr := newOfficialScheduleSourceError(officialScheduleReasonRequest, 0, fmt.Errorf("create official schedule API request: %w", err))

		return nil, fmt.Errorf("official schedule source error: %w", sourceErr)
	}

	req.Header.Set("Accept", contentTypeJSON)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; HololiveBot/1.0)")

	return req, nil
}

func validateOfficialScheduleAPIBaseURL(baseURL *url.URL) error {
	if baseURL == nil || baseURL.Scheme != "https" || baseURL.Host == "" {
		return errors.New("official schedule base URL must be an HTTPS origin")
	}

	if baseURL.User != nil || (baseURL.Path != "" && baseURL.Path != "/") {
		return errors.New("official schedule base URL must not contain userinfo or path")
	}

	if baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return errors.New("official schedule base URL must not contain query or fragment")
	}

	return nil
}

func (s *Service) executeOfficialScheduleAPIRequest(req *http.Request) ([]byte, error) {
	if s.httpClient == nil {
		if err := newOfficialScheduleSourceError(officialScheduleReasonTransport, 0, errors.New("official schedule HTTP client is nil")); err != nil {
			return nil, fmt.Errorf("official schedule source error: %w", err)
		}

		return nil, nil
	}

	resp, err := s.httpClient.Do(req) //nolint:bodyclose // downstream helpers own every non-nil response body path
	if err != nil {
		if officialErr := officialScheduleRequestFailure(resp, err); officialErr != nil {
			return nil, fmt.Errorf("official schedule request failure: %w", officialErr)
		}

		return nil, nil
	}

	if validateOfficialErr := validateOfficialScheduleResponse(resp); validateOfficialErr != nil {
		return nil, fmt.Errorf("validate official schedule response: %w", validateOfficialErr)
	}

	out, err := s.readOfficialScheduleAPIResponse(resp)
	if err != nil {
		return out, fmt.Errorf("read official schedule API response: %w", err)
	}

	return out, nil
}

func officialScheduleRequestFailure(resp *http.Response, requestErr error) error {
	if resp != nil && resp.Body != nil {
		requestErr = errors.Join(requestErr, httputil.DrainAndClose(resp.Body, httputil.DefaultDrainLimit))
	}

	reason := officialScheduleReasonTransport

	if errors.Is(requestErr, context.Canceled) || errors.Is(requestErr, context.DeadlineExceeded) {
		reason = officialScheduleReasonContext
	}

	if err := newOfficialScheduleSourceError(reason, 0, fmt.Errorf("request official schedule API: %w", requestErr)); err != nil {
		return fmt.Errorf("official schedule source error: %w", err)
	}

	return nil
}

func validateOfficialScheduleResponse(resp *http.Response) error {
	if resp == nil {
		if err := newOfficialScheduleSourceError(officialScheduleReasonTransport, 0, errors.New("request official schedule API: nil response")); err != nil {
			return fmt.Errorf("official schedule source error: %w", err)
		}

		return nil
	}

	if resp.Body == nil {
		if err := newOfficialScheduleSourceError(officialScheduleReasonTransport, resp.StatusCode, errors.New("request official schedule API: nil response body")); err != nil {
			return fmt.Errorf("official schedule source error: %w", err)
		}

		return nil
	}

	return nil
}

func (s *Service) readOfficialScheduleAPIResponse(resp *http.Response) ([]byte, error) {
	if err := validateOfficialScheduleStatus(resp); err != nil {
		return nil, fmt.Errorf("validate official schedule status: %w", err)
	}

	if err := validateOfficialScheduleResponseContentType(resp); err != nil {
		return nil, fmt.Errorf("validate official schedule response content type: %w", err)
	}

	out, err := s.readOfficialScheduleResponseBody(resp)
	if err != nil {
		return out, fmt.Errorf("read official schedule response body: %w", err)
	}

	return out, nil
}

func validateOfficialScheduleStatus(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		if err := validateOfficialScheduleResponse(resp); err != nil {
			return fmt.Errorf("validate official schedule response: %w", err)
		}

		return nil
	}

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	closeErr := httputil.DrainAndClose(resp.Body, httputil.DefaultDrainLimit)
	statusErr := fmt.Errorf("official schedule API returned status %d", resp.StatusCode)

	if err := newOfficialScheduleSourceError(officialScheduleReasonStatus, resp.StatusCode, errors.Join(statusErr, closeErr)); err != nil {
		return fmt.Errorf("official schedule source error: %w", err)
	}

	return nil
}

func validateOfficialScheduleResponseContentType(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		if err := validateOfficialScheduleResponse(resp); err != nil {
			return fmt.Errorf("validate official schedule response: %w", err)
		}

		return nil
	}

	if err := validateOfficialScheduleContentType(resp.Header.Get("Content-Type")); err != nil {
		closeErr := httputil.DrainAndClose(resp.Body, httputil.DefaultDrainLimit)
		if newOfficialScheduleSourceErr := newOfficialScheduleSourceError(officialScheduleReasonContentType, resp.StatusCode, errors.Join(err, closeErr)); newOfficialScheduleSourceErr != nil {
			return fmt.Errorf("official schedule source error: %w", newOfficialScheduleSourceErr)
		}

		return nil
	}

	return nil
}
