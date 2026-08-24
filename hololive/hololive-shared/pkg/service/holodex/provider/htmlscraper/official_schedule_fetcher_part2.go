package htmlscraper

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (s *Service) readOfficialScheduleResponseBody(resp *http.Response) ([]byte, error) {
	if resp == nil || resp.Body == nil {
		if err := validateOfficialScheduleResponse(resp); err != nil {
			return nil, fmt.Errorf("validate official schedule response: %w", err)
		}

		return nil, nil
	}

	body, err := httputil.ReadAllAndClose(resp.Body, s.maxResponseBodyBytes)
	if err != nil {
		if newErr2 := newOfficialScheduleSourceError(
			officialScheduleBodyReadReason(err),
			resp.StatusCode,
			fmt.Errorf("read official schedule API response: %w", err),
		); newErr2 != nil {
			return nil, fmt.Errorf("official schedule source error: %w", newErr2)
		}

		return nil, nil
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

	if !strings.EqualFold(mediaType, contentTypeJSON) {
		return fmt.Errorf("unexpected official schedule content type: %s", mediaType)
	}

	return nil
}

func newOfficialScheduleSourceError(reason officialScheduleReason, statusCode int, err error) error {
	if err == nil {
		return nil
	}

	return &officialScheduleSourceError{reason: reason, statusCode: statusCode, err: err}
}

func (s *Service) logOfficialScheduleAPIError(err error) {
	if sourceErr, ok := errors.AsType[*officialScheduleSourceError](err); ok {
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
