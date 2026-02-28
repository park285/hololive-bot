package majorevent

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const defaultRSSURL = "https://hololive.hololivepro.com/events/feed/"

// HTTPClient: HTTP 요청을 수행하는 인터페이스
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Service: 대형 행사 정보를 관리하는 서비스
type Service struct {
	httpClient    HTTPClient
	rssParser     *RSSParser
	dateExtractor *DateExtractor
	rssURL        string
	logger        *slog.Logger
}

// ServiceOption: Service 설정 옵션
type ServiceOption func(*Service)

// WithRSSURL: RSS URL을 설정합니다.
func WithRSSURL(url string) ServiceOption {
	return func(s *Service) {
		s.rssURL = url
	}
}

// WithLogger: 로거를 설정합니다.
func WithLogger(logger *slog.Logger) ServiceOption {
	return func(s *Service) {
		s.logger = logger
	}
}

// NewService: MajorEvent 서비스 인스턴스를 생성합니다.
func NewService(httpClient HTTPClient, opts ...ServiceOption) *Service {
	s := &Service{
		httpClient:    httpClient,
		rssParser:     NewRSSParser(),
		dateExtractor: NewDateExtractor(),
		rssURL:        defaultRSSURL,
		logger:        slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// FetchEvents: RSS Feed에서 대형 행사 목록을 가져옵니다.
func (s *Service) FetchEvents(ctx context.Context) ([]domain.MajorEvent, error) {
	var lastErr error
	maxRetries := constants.MajorEventConfig.MaxRetries
	retryDelay := constants.MajorEventConfig.RetryDelay

	for i := range maxRetries {
		events, err := s.fetchEventsOnce(ctx)
		if err == nil {
			return events, nil
		}

		lastErr = err
		s.logger.Warn("fetch events failed, retrying",
			slog.Int("attempt", i+1),
			slog.Int("max_retries", maxRetries),
			slog.String("error", err.Error()))

		if i < maxRetries-1 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("context canceled: %w", ctx.Err())
			case <-time.After(retryDelay):
			}
		}
	}

	s.logger.Error("fetch events failed after retries", slog.String("error", lastErr.Error()))
	return nil, fmt.Errorf("fetch events after %d retries: %w", maxRetries, lastErr)
}

func (s *Service) fetchEventsOnce(ctx context.Context) ([]domain.MajorEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.rssURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := jsonutil.ReadAllLimit(resp.Body, constants.APIConfig.MaxResponseBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	events, err := s.rssParser.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse RSS: %w", err)
	}

	// 각 이벤트의 Description에서 날짜 추출
	for i := range events {
		dates := s.dateExtractor.Extract(events[i].Description)
		events[i].EventDates = dates
	}

	return events, nil
}

// FilterWeeklyEvents: 주어진 기간 내의 행사만 필터링합니다.
func (s *Service) FilterWeeklyEvents(events []domain.MajorEvent, weekStart, weekEnd time.Time) []domain.MajorEvent {
	filtered := make([]domain.MajorEvent, 0)

	for i := range events {
		if s.isEventInRange(&events[i], weekStart, weekEnd) {
			filtered = append(filtered, events[i])
		}
	}

	return filtered
}

func (s *Service) isEventInRange(event *domain.MajorEvent, start, end time.Time) bool {
	if !event.HasEventDates() {
		return false
	}

	// 시작일 기준으로 판단 (멀티데이 이벤트의 경우)
	firstDate := event.EventDates[0]
	return !firstDate.Before(start) && !firstDate.After(end)
}

// GetWeekRange: 이번 주 월요일 00:00 KST ~ 일요일 23:59 KST 범위를 계산합니다.
// 월요일 발송 기준: 발송 당일(월)부터 일요일까지의 이벤트를 대상으로 합니다.
func GetWeekRange(now time.Time) (start, end time.Time) {
	nowKST := now.In(kst)

	// 이번 주 월요일 찾기 (월요일이면 당일)
	daysFromMonday := (int(nowKST.Weekday()) - int(time.Monday) + 7) % 7
	monday := time.Date(
		nowKST.Year(), nowKST.Month(), nowKST.Day()-daysFromMonday,
		0, 0, 0, 0, kst,
	)

	// 같은 주 일요일 23:59:59
	sunday := monday.AddDate(0, 0, 6)
	sundayEnd := time.Date(
		sunday.Year(), sunday.Month(), sunday.Day(),
		23, 59, 59, 0, kst,
	)

	return monday, sundayEnd
}
