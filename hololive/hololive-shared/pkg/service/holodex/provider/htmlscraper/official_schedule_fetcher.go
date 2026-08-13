package htmlscraper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/httputil"
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
	return s.waitOfficialScheduleResult(ctx, resultCh)
}

func (s *Service) loadOfficialScheduleOrigin(ctx context.Context) ([]*domain.Stream, error) {
	if cached, ok := s.getOfficialPageCache(); ok {
		return cached, nil
	}

	originCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.officialSchedule.Timeout)
	defer cancel()
	streams, err := s.fetchOfficialScheduleAPI(originCtx)
	if err != nil {
		return nil, err
	}
	s.setOfficialPageCache(streams)
	return streams, nil
}

func (s *Service) waitOfficialScheduleResult(ctx context.Context, resultCh <-chan singleflight.Result) ([]*domain.Stream, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for official schedule API: %w", ctx.Err())
	case result := <-resultCh:
		return s.resolveOfficialScheduleResult(result.Val, result.Err, result.Shared)
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
		return nil, err
	}
	body, err := s.executeOfficialScheduleAPIRequest(req)
	if err != nil {
		reason = classifyOfficialScheduleReason(err, 0)
		s.logOfficialScheduleAPIError(err)
		return nil, err
	}

	streams, stats, err := s.decodeOfficialScheduleAPI(body)
	observeOfficialScheduleRows(stats)
	if err != nil {
		reason = classifyOfficialScheduleReason(err, 0)
		s.logOfficialScheduleAPIError(err)
		return nil, err
	}

	outcome = "success"
	reason = classifyOfficialScheduleReason(nil, len(streams))
	markOfficialScheduleSuccess()
	return streams, nil
}

func (s *Service) newOfficialScheduleAPIRequest(ctx context.Context) (*http.Request, error) {
	baseURL, err := url.Parse(strings.TrimSpace(s.officialSchedule.BaseURL))
	if err != nil {
		return nil, newOfficialScheduleSourceError(officialScheduleReasonRequest, 0, fmt.Errorf("parse official schedule base URL: %w", err))
	}
	if err := validateOfficialScheduleAPIBaseURL(baseURL); err != nil {
		return nil, newOfficialScheduleSourceError(officialScheduleReasonRequest, 0, err)
	}
	baseURL.Path = officialScheduleAPIPath
	baseURL.RawQuery = ""
	baseURL.Fragment = ""

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL.String(), http.NoBody)
	if err != nil {
		return nil, newOfficialScheduleSourceError(officialScheduleReasonRequest, 0, fmt.Errorf("create official schedule API request: %w", err))
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; HololiveBot/1.0)")
	return req, nil
}

func validateOfficialScheduleAPIBaseURL(baseURL *url.URL) error {
	if baseURL == nil || baseURL.Scheme != "https" || baseURL.Host == "" {
		return fmt.Errorf("official schedule base URL must be an HTTPS origin")
	}
	if baseURL.User != nil || (baseURL.Path != "" && baseURL.Path != "/") {
		return fmt.Errorf("official schedule base URL must not contain userinfo or path")
	}
	if baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return fmt.Errorf("official schedule base URL must not contain query or fragment")
	}
	return nil
}

func (s *Service) executeOfficialScheduleAPIRequest(req *http.Request) ([]byte, error) {
	if s.httpClient == nil {
		return nil, newOfficialScheduleSourceError(officialScheduleReasonTransport, 0, fmt.Errorf("official schedule HTTP client is nil"))
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, officialScheduleRequestFailure(resp, err)
	}
	if err := validateOfficialScheduleResponse(resp); err != nil {
		return nil, err
	}
	return s.readOfficialScheduleAPIResponse(resp)
}

func officialScheduleRequestFailure(resp *http.Response, requestErr error) error {
	if resp != nil && resp.Body != nil {
		requestErr = errors.Join(requestErr, httputil.DrainAndClose(resp.Body, httputil.DefaultDrainLimit))
	}
	reason := officialScheduleReasonTransport
	if errors.Is(requestErr, context.Canceled) || errors.Is(requestErr, context.DeadlineExceeded) {
		reason = officialScheduleReasonContext
	}
	return newOfficialScheduleSourceError(reason, 0, fmt.Errorf("request official schedule API: %w", requestErr))
}

func validateOfficialScheduleResponse(resp *http.Response) error {
	if resp == nil {
		return newOfficialScheduleSourceError(officialScheduleReasonTransport, 0, fmt.Errorf("request official schedule API: nil response"))
	}
	if resp.Body == nil {
		return newOfficialScheduleSourceError(officialScheduleReasonTransport, resp.StatusCode, fmt.Errorf("request official schedule API: nil response body"))
	}
	return nil
}

func (s *Service) readOfficialScheduleAPIResponse(resp *http.Response) ([]byte, error) {
	if err := validateOfficialScheduleStatus(resp); err != nil {
		return nil, err
	}
	if err := validateOfficialScheduleResponseContentType(resp); err != nil {
		return nil, err
	}
	return s.readOfficialScheduleResponseBody(resp)
}

func validateOfficialScheduleStatus(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	closeErr := httputil.DrainAndClose(resp.Body, httputil.DefaultDrainLimit)
	statusErr := fmt.Errorf("official schedule API returned status %d", resp.StatusCode)
	return newOfficialScheduleSourceError(officialScheduleReasonStatus, resp.StatusCode, errors.Join(statusErr, closeErr))
}

func validateOfficialScheduleResponseContentType(resp *http.Response) error {
	if err := validateOfficialScheduleContentType(resp.Header.Get("Content-Type")); err != nil {
		closeErr := httputil.DrainAndClose(resp.Body, httputil.DefaultDrainLimit)
		return newOfficialScheduleSourceError(officialScheduleReasonContentType, resp.StatusCode, errors.Join(err, closeErr))
	}
	return nil
}

func (s *Service) readOfficialScheduleResponseBody(resp *http.Response) ([]byte, error) {
	body, err := httputil.ReadAllAndClose(resp.Body, s.maxResponseBodyBytes)
	if err != nil {
		return nil, newOfficialScheduleSourceError(
			officialScheduleBodyReadReason(err),
			resp.StatusCode,
			fmt.Errorf("read official schedule API response: %w", err),
		)
	}
	observeOfficialScheduleResponseBytes(len(body))
	return body, nil
}

func officialScheduleBodyReadReason(err error) officialScheduleReason {
	if errors.Is(err, httputil.ErrResponseBodyTooLarge) {
		return officialScheduleReasonOversize
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return officialScheduleReasonContext
	}
	return officialScheduleReasonTransport
}

func validateOfficialScheduleContentType(contentType string) error {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("parse official schedule content type: %w", err)
	}
	if !strings.EqualFold(mediaType, "application/json") {
		return fmt.Errorf("unexpected official schedule content type: %s", mediaType)
	}
	return nil
}

func (s *Service) decodeOfficialScheduleAPI(body []byte) ([]*domain.Stream, officialScheduleRowStats, error) {
	groups, err := decodeOfficialScheduleGroups(body)
	if err != nil {
		return nil, officialScheduleRowStats{}, err
	}
	rows, err := decodeOfficialScheduleRows(groups)
	if err != nil {
		return nil, officialScheduleRowStats{}, err
	}
	return s.mapOfficialScheduleRows(rows)
}

func decodeOfficialScheduleGroups(body []byte) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(body)
	if !json.Valid(trimmed) {
		return nil, newOfficialScheduleSourceError(officialScheduleReasonDecode, 0, fmt.Errorf("decode official schedule response: invalid JSON"))
	}
	if len(trimmed) == 0 || trimmed[0] != 0x7b {
		return nil, newOfficialScheduleSourceError(officialScheduleReasonSchema, 0, fmt.Errorf("official schedule response root must be an object"))
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &root); err != nil {
		return nil, newOfficialScheduleSourceError(officialScheduleReasonDecode, 0, fmt.Errorf("decode official schedule response: %w", err))
	}
	rawGroups, ok := root["dateGroupList"]
	if !ok {
		return nil, newOfficialScheduleSourceError(officialScheduleReasonSchema, 0, fmt.Errorf("official schedule response missing dateGroupList"))
	}
	groups, err := decodeRawJSONArray(rawGroups, "dateGroupList")
	if err != nil {
		return nil, err
	}
	return groups, nil
}

func decodeOfficialScheduleRows(groups []json.RawMessage) ([]json.RawMessage, error) {
	rows := make([]json.RawMessage, 0)
	for index, rawGroup := range groups {
		var group map[string]json.RawMessage
		if err := json.Unmarshal(rawGroup, &group); err != nil {
			return nil, newOfficialScheduleSourceError(officialScheduleReasonSchema, 0, fmt.Errorf("decode official schedule group %d: %w", index, err))
		}
		rawRows, ok := group["videoList"]
		if !ok {
			return nil, newOfficialScheduleSourceError(officialScheduleReasonSchema, 0, fmt.Errorf("official schedule group %d missing videoList", index))
		}
		groupRows, err := decodeRawJSONArray(rawRows, fmt.Sprintf("dateGroupList[%d].videoList", index))
		if err != nil {
			return nil, err
		}
		rows = append(rows, groupRows...)
	}
	return rows, nil
}

func decodeRawJSONArray(raw json.RawMessage, field string) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, newOfficialScheduleSourceError(officialScheduleReasonSchema, 0, fmt.Errorf("official schedule field %s must be an array", field))
	}

	var values []json.RawMessage
	if err := json.Unmarshal(trimmed, &values); err != nil {
		return nil, newOfficialScheduleSourceError(officialScheduleReasonSchema, 0, fmt.Errorf("decode official schedule field %s: %w", field, err))
	}
	return values, nil
}

func newOfficialScheduleSourceError(reason officialScheduleReason, statusCode int, err error) error {
	if err == nil {
		return nil
	}
	return &officialScheduleSourceError{reason: reason, statusCode: statusCode, err: err}
}

func (s *Service) logOfficialScheduleAPIError(err error) {
	var sourceErr *officialScheduleSourceError
	if errors.As(err, &sourceErr) {
		s.logger.Warn("Official schedule API request failed",
			slog.String("reason", string(sourceErr.reason)),
			slog.Int("status_code", sourceErr.statusCode),
			slog.Any("error", sourceErr.err))
		return
	}
	s.logger.Warn("Official schedule API processing failed",
		slog.String("reason", string(classifyOfficialScheduleReason(err, 0))),
		slog.Any("error", err))
}

func (s *Service) getOfficialPageCache() ([]*domain.Stream, bool) {
	ttl := s.officialSchedule.PageCacheTTL
	if ttl <= 0 {
		return nil, false
	}

	now := s.now()
	s.officialPageMu.RLock()
	defer s.officialPageMu.RUnlock()
	if s.officialPage.expiresAt.IsZero() || !now.Before(s.officialPage.expiresAt) {
		return nil, false
	}
	return cloneStreams(s.officialPage.streams), true
}

func (s *Service) setOfficialPageCache(streams []*domain.Stream) {
	if s.officialSchedule.PageCacheTTL <= 0 {
		return
	}

	s.officialPageMu.Lock()
	s.officialPage = officialSchedulePageCache{
		streams:   cloneStreams(streams),
		expiresAt: s.now().Add(s.officialSchedule.PageCacheTTL),
	}
	s.officialPageMu.Unlock()
}

func (s *Service) now() time.Time {
	if s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now()
}

func cloneStreams(streams []*domain.Stream) []*domain.Stream {
	if streams == nil {
		return nil
	}
	cloned := make([]*domain.Stream, 0, len(streams))
	for _, stream := range streams {
		cloned = append(cloned, cloneStream(stream))
	}
	return cloned
}

func cloneStream(stream *domain.Stream) *domain.Stream {
	if stream == nil {
		return nil
	}
	cloned := *stream
	cloned.StartScheduled = cloneTimePtr(stream.StartScheduled)
	cloned.StartActual = cloneTimePtr(stream.StartActual)
	cloned.Duration = cloneIntPtr(stream.Duration)
	cloned.Thumbnail = cloneStringPtr(stream.Thumbnail)
	cloned.Link = cloneStringPtr(stream.Link)
	cloned.TopicID = cloneStringPtr(stream.TopicID)
	cloned.Channel = cloneChannelPtr(stream.Channel)
	cloned.ViewerCount = cloneIntPtr(stream.ViewerCount)
	return &cloned
}

func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneChannelPtr(value *domain.Channel) *domain.Channel {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.EnglishName = cloneStringPtr(value.EnglishName)
	cloned.Photo = cloneStringPtr(value.Photo)
	cloned.Twitter = cloneStringPtr(value.Twitter)
	cloned.VideoCount = cloneIntPtr(value.VideoCount)
	cloned.SubscriberCount = cloneIntPtr(value.SubscriberCount)
	cloned.Org = cloneStringPtr(value.Org)
	cloned.Suborg = cloneStringPtr(value.Suborg)
	cloned.Group = cloneStringPtr(value.Group)
	return &cloned
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
