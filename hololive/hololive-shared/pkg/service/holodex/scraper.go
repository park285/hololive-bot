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

package holodex

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
	"golang.org/x/sync/singleflight"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/fallback"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

// 폴백 순서: YouTube HTML 스크래핑 → 홀로라이브 공식 스케줄 페이지
type ScraperService struct {
	httpClient     *http.Client
	cache          cache.Client
	membersData    domain.MemberDataProvider
	memberNameMap  map[string]string // memberName -> channelID
	logger         *slog.Logger
	baseURL        string
	youtubeScraper *scraper.Client // YouTube HTML 스크래퍼 (1차 폴백)
	fetchUpcoming  func(ctx context.Context, channelID string) ([]*scraper.UpcomingEvent, error)
	officialPageMu sync.RWMutex
	officialPage   officialSchedulePageCache
	officialGroup  singleflight.Group
	nowFunc        func() time.Time
}

const (
	scraperChannelCacheKeyPrefix = "scraper:channel:"
	officialSchedulePageCacheKey = "official_schedule_page"
)

type officialSchedulePageCache struct {
	streams   []*domain.Stream
	expiresAt time.Time
}

// 멤버 정보와 매핑 데이터를 초기화하여 크롤링 데이터 파싱에 활용한다.
func NewScraperService(
	cacheSvc cache.Client,
	membersData domain.MemberDataProvider,
	youtubeProxyConfig scraper.ProxyConfig,
	sharedRL *scraper.RateLimiter,
	logger *slog.Logger,
) *ScraperService {
	nameMap := make(map[string]string)

	for _, member := range membersData.GetAllMembers() {
		nameMap[stringutil.Normalize(member.Name)] = member.ChannelID

		if member.NameJa != "" {
			nameMap[stringutil.Normalize(member.NameJa)] = member.ChannelID
		}

		if member.Aliases != nil {
			for _, alias := range member.Aliases.Ko {
				nameMap[stringutil.Normalize(alias)] = member.ChannelID
			}
			for _, alias := range member.Aliases.Ja {
				nameMap[stringutil.Normalize(alias)] = member.ChannelID
			}
		}
	}

	logger.Info("Scraper initialized with member matching and YouTube fallback",
		slog.Int("members", len(membersData.GetAllMembers())),
		slog.Int("name_mappings", len(nameMap)))

	return &ScraperService{
		httpClient: &http.Client{
			Timeout: constants.OfficialScheduleConfig.Timeout,
		},
		cache:          cacheSvc,
		membersData:    membersData,
		memberNameMap:  nameMap,
		logger:         logger,
		baseURL:        constants.OfficialScheduleConfig.BaseURL,
		youtubeScraper: scraper.NewClient(scraper.WithProxy(youtubeProxyConfig), scraper.WithRateLimiter(sharedRL)), // YouTube HTML 스크래퍼 초기화
	}
}

// 우선 YouTube HTML 스크래핑을 사용하고, 스크래퍼 오류 시에만 공식 스케줄 페이지로 전환한다.
func (s *ScraperService) FetchChannel(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	cacheKey := scraperChannelCacheKeyPrefix + channelID
	if cached, found := s.cache.GetStreams(ctx, cacheKey); found {
		s.logger.Debug("Scraper cache hit", slog.String("channel", channelID))
		return cached, nil
	}

	// Phase 1: YouTube HTML 스크래핑 시도
	if s.youtubeScraper != nil || s.fetchUpcoming != nil {
		policy := fallback.Policy{Trigger: fallback.TriggerOnFailures}
		streams, err := s.fetchFromYouTubeScraper(ctx, channelID)
		fallback.ObservePrimaryPhase("holodex", "channel_schedule", 1, boolToInt(len(streams) > 0), boolToInt(err != nil))
		if !policy.ShouldRun(len(streams), boolToInt(err != nil)) {
			fallback.ObserveExecution("holodex", "channel_schedule", policy.Trigger, "skipped")
			s.cache.SetStreams(ctx, cacheKey, streams, constants.OfficialScheduleConfig.CacheExpiry)
			s.logger.Info("YouTube scraper channel schedule resolved",
				slog.String("channel", channelID),
				slog.Int("streams", len(streams)))
			return streams, nil
		}
		if err != nil {
			s.logger.Debug("YouTube scraper failed, falling back to official schedule",
				slog.String("channel", channelID),
				slog.Any("error", err))
		}
	}

	// Phase 2: 홀로라이브 공식 스케줄 페이지 (스크래퍼 오류 시에만 사용)
	s.logger.Info("Fetching from official schedule (FALLBACK MODE)",
		slog.String("channel", channelID),
		slog.String("url", s.baseURL))

	allStreams, err := s.fetchAllStreams(ctx)
	if err != nil {
		fallback.ObserveExecution("holodex", "channel_schedule", fallback.TriggerOnFailures, "error")
		return nil, fmt.Errorf("all fallback sources failed: %w", err)
	}

	channelStreams := make([]*domain.Stream, 0)
	for _, stream := range allStreams {
		if stream.ChannelID == channelID {
			channelStreams = append(channelStreams, stream)
		}
	}

	s.cache.SetStreams(ctx, cacheKey, channelStreams, constants.OfficialScheduleConfig.CacheExpiry)
	if len(channelStreams) > 0 {
		fallback.ObserveExecution("holodex", "channel_schedule", fallback.TriggerOnFailures, "hit")
	} else {
		fallback.ObserveExecution("holodex", "channel_schedule", fallback.TriggerOnFailures, "miss")
	}

	s.logger.Info("Official schedule scraper completed",
		slog.String("channel", channelID),
		slog.Int("streams", len(channelStreams)))

	return channelStreams, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// fetchFromYouTubeScraper: YouTube HTML 직접 스크래핑으로 예정/라이브 방송 조회
func (s *ScraperService) fetchFromYouTubeScraper(ctx context.Context, channelID string) ([]*domain.Stream, error) {
	var (
		events []*scraper.UpcomingEvent
		err    error
	)
	switch {
	case s.fetchUpcoming != nil:
		events, err = s.fetchUpcoming(ctx, channelID)
	case s.youtubeScraper != nil:
		events, err = s.youtubeScraper.GetUpcomingEvents(ctx, channelID)
	default:
		return nil, fmt.Errorf("youtube scraper not configured")
	}
	if err != nil {
		return nil, fmt.Errorf("youtube scraper error: %w", err)
	}

	streams := make([]*domain.Stream, 0, len(events))
	for _, event := range events {
		stream := s.convertEventToStream(event, channelID)
		streams = append(streams, stream)
	}

	return streams, nil
}

// convertEventToStream: YouTube scraper의 UpcomingEvent를 domain.Stream으로 변환
func (s *ScraperService) convertEventToStream(event *scraper.UpcomingEvent, channelID string) *domain.Stream {
	stream := &domain.Stream{
		ID:          event.VideoID,
		Title:       event.Title,
		ChannelID:   channelID,
		ChannelName: event.ChannelTitle,
		Status:      s.mapEventStatus(event.Status),
	}

	// 썸네일 설정
	if len(event.Thumbnail) > 0 {
		// 가장 큰 썸네일 선택
		bestThumb := event.Thumbnail[len(event.Thumbnail)-1].URL
		stream.Thumbnail = &bestThumb
	} else {
		thumbURL := fmt.Sprintf("https://i.ytimg.com/vi/%s/maxresdefault.jpg", event.VideoID)
		stream.Thumbnail = &thumbURL
	}

	// 링크 설정
	linkURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", event.VideoID)
	stream.Link = &linkURL

	// 시작 시간 설정
	if event.StartTime != nil {
		startTime := time.Unix(*event.StartTime, 0)
		stream.StartScheduled = &startTime
	}

	return stream
}

// mapEventStatus: YouTube scraper 상태를 domain.StreamStatus로 변환
func (s *ScraperService) mapEventStatus(status string) domain.StreamStatus {
	switch status {
	case "LIVE":
		return domain.StreamStatusLive
	case "UPCOMING":
		return domain.StreamStatusUpcoming
	default:
		return domain.StreamStatusUpcoming
	}
}

func (s *ScraperService) FetchAllStreams(ctx context.Context) ([]*domain.Stream, error) {
	return s.fetchAllStreams(ctx)
}

func (s *ScraperService) SetYouTubeProxyEnabled(enabled bool) bool {
	if s == nil || s.youtubeScraper == nil {
		return false
	}
	return s.youtubeScraper.SetProxyEnabled(enabled)
}

func (s *ScraperService) YouTubeProxyEnabled() bool {
	if s == nil || s.youtubeScraper == nil {
		return false
	}
	return s.youtubeScraper.ProxyEnabled()
}

func (s *ScraperService) fetchAllStreams(ctx context.Context) ([]*domain.Stream, error) {
	if cached, ok := s.getOfficialPageCache(); ok {
		s.logger.Debug("Official schedule page cache hit",
			slog.String("key", officialSchedulePageCacheKey),
			slog.Int("streams", len(cached)))
		return cached, nil
	}

	result, err, shared := s.officialGroup.Do(officialSchedulePageCacheKey, func() (any, error) {
		if cached, ok := s.getOfficialPageCache(); ok {
			return cached, nil
		}

		streams, fetchErr := s.fetchAllStreamsFromOrigin(ctx)
		if fetchErr != nil {
			return nil, fetchErr
		}

		s.setOfficialPageCache(streams)
		return streams, nil
	})
	if err != nil {
		return nil, fmt.Errorf("load official schedule page: %w", err)
	}

	streams, ok := result.([]*domain.Stream)
	if !ok {
		return nil, fmt.Errorf("invalid official schedule page cache result: %T", result)
	}
	if shared {
		s.logger.Debug("Official schedule page request deduplicated",
			slog.String("key", officialSchedulePageCacheKey),
			slog.Int("streams", len(streams)))
	}
	return cloneStreams(streams), nil
}

func (s *ScraperService) fetchAllStreamsFromOrigin(ctx context.Context) ([]*domain.Stream, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.baseURL+"/lives/hololive", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create scraper request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; HololiveBot/1.0)")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("HTML parse failed: %w", err)
	}

	streams := make([]*domain.Stream, 0)
	parseErrors := 0
	currentDate := ""

	doc.Find(".container .col-12").Each(func(i int, container *goquery.Selection) {
		dateHeader := container.Find(".navbar-inverse .holodule.navbar-text")
		if dateHeader.Length() > 0 {
			dateText := stringutil.TrimSpace(dateHeader.Text())
			dateText = strings.Split(dateText, "(")[0]
			currentDate = stringutil.TrimSpace(dateText)
			s.logger.Debug("Found date section", slog.String("date", currentDate))
			return
		}

		container.Find("a.thumbnail").Each(func(j int, sel *goquery.Selection) {
			stream, err := s.parseStreamElement(sel, currentDate)
			if err != nil {
				parseErrors++
				s.logger.Debug("Failed to parse stream element",
					slog.String("date", currentDate),
					slog.Any("error", err))
				return
			}

			if stream != nil {
				streams = append(streams, stream)
			}
		})
	})

	if len(streams) == 0 {
		return nil, &StructureChangedError{
			Message:     "No streams found - HTML structure may have changed",
			ParseErrors: parseErrors,
		}
	}

	if parseErrors > len(streams)/2 {
		s.logger.Warn("High parse error rate detected",
			slog.Int("successes", len(streams)),
			slog.Int("errors", parseErrors))
	}

	s.logger.Info("Scraper fetched all streams",
		slog.Int("total", len(streams)),
		slog.Int("parse_errors", parseErrors))

	return streams, nil
}

func (s *ScraperService) getOfficialPageCache() ([]*domain.Stream, bool) {
	ttl := constants.OfficialScheduleConfig.PageCacheTTL
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

func (s *ScraperService) setOfficialPageCache(streams []*domain.Stream) {
	ttl := constants.OfficialScheduleConfig.PageCacheTTL
	if ttl <= 0 {
		return
	}

	s.officialPageMu.Lock()
	defer s.officialPageMu.Unlock()

	s.officialPage = officialSchedulePageCache{
		streams:   cloneStreams(streams),
		expiresAt: s.now().Add(ttl),
	}
}

func (s *ScraperService) now() time.Time {
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

	if stream.StartScheduled != nil {
		startScheduled := *stream.StartScheduled
		cloned.StartScheduled = &startScheduled
	}
	if stream.StartActual != nil {
		startActual := *stream.StartActual
		cloned.StartActual = &startActual
	}
	if stream.Duration != nil {
		duration := *stream.Duration
		cloned.Duration = &duration
	}
	if stream.Thumbnail != nil {
		thumbnail := *stream.Thumbnail
		cloned.Thumbnail = &thumbnail
	}
	if stream.Link != nil {
		link := *stream.Link
		cloned.Link = &link
	}
	if stream.TopicID != nil {
		topicID := *stream.TopicID
		cloned.TopicID = &topicID
	}
	if stream.Channel != nil {
		channel := *stream.Channel
		cloned.Channel = &channel
	}
	if stream.ViewerCount != nil {
		viewerCount := *stream.ViewerCount
		cloned.ViewerCount = &viewerCount
	}

	return &cloned
}

func (s *ScraperService) parseStreamElement(sel *goquery.Selection, currentDate string) (*domain.Stream, error) {
	videoURL, exists := sel.Attr("href")
	if !exists || !strings.Contains(videoURL, "youtube.com/watch?v=") {
		return nil, fmt.Errorf("invalid video URL")
	}

	videoID := s.extractVideoID(videoURL)
	if videoID == "" {
		return nil, fmt.Errorf("could not extract video ID from %s", videoURL)
	}

	timeText := stringutil.TrimSpace(sel.Find(".datetime").Text())
	memberName := stringutil.TrimSpace(sel.Find(".name").Text())
	if memberName == "" {
		memberName = stringutil.TrimSpace(sel.Find(".text").Text())
	}

	if onclickStr, exists := sel.Attr("onclick"); exists {
		if extractedName := s.extractMemberFromOnClick(onclickStr); extractedName != "" {
			memberName = extractedName
		}
	}

	startTime, err := s.parseDatetimeWithContext(currentDate, timeText)
	if err != nil {
		s.logger.Debug("Failed to parse datetime",
			slog.String("date", currentDate),
			slog.String("time", timeText),
			slog.Any("error", err))
	}

	thumbnailURL := fmt.Sprintf("https://img.youtube.com/vi/%s/maxresdefault.jpg", videoID)

	channelID := s.matchMemberToChannel(memberName)
	if channelID == "" {
		s.logger.Debug("Could not match member name to channel ID",
			slog.String("member_name", memberName),
			slog.String("video_id", videoID))
	}

	stream := &domain.Stream{
		ID:             videoID,
		Title:          memberName,
		ChannelID:      channelID,
		ChannelName:    memberName,
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: startTime,
		Link:           &videoURL,
		Thumbnail:      &thumbnailURL,
	}

	return stream, nil
}

func (s *ScraperService) matchMemberToChannel(memberName string) string {
	if memberName == "" {
		return ""
	}

	normalized := stringutil.Normalize(memberName)
	if channelID, found := s.memberNameMap[normalized]; found {
		return channelID
	}

	for name, channelID := range s.memberNameMap {
		if strings.Contains(name, normalized) || strings.Contains(normalized, name) {
			s.logger.Debug("Matched member via partial match",
				slog.String("scraped", memberName),
				slog.String("matched", name),
				slog.String("channel_id", channelID))
			return channelID
		}
	}

	return ""
}

func (s *ScraperService) extractVideoID(videoURL string) string {
	parts := strings.Split(videoURL, "?v=")
	if len(parts) < 2 {
		return ""
	}

	videoID := parts[1]
	if idx := strings.Index(videoID, "&"); idx != -1 {
		videoID = videoID[:idx]
	}

	return videoID
}

func (s *ScraperService) parseDatetimeWithContext(date, timeStr string) (*time.Time, error) {
	date = stringutil.TrimSpace(date)
	timeStr = stringutil.TrimSpace(timeStr)

	if date == "" || timeStr == "" {
		return nil, fmt.Errorf("empty date or time")
	}

	combined := fmt.Sprintf("%s %s", date, timeStr)

	// JST 타임존 로드 (Alpine에 tzdata 없을 경우 FixedZone 폴백)
	jst, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		s.logger.Debug("Failed to load Asia/Tokyo timezone, using FixedZone", slog.Any("error", err))
		jst = time.FixedZone("JST", 9*60*60) // UTC+9
	}
	now := time.Now().In(jst)

	t, err := time.ParseInLocation("01/02 15:04", combined, jst)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", combined, err)
	}

	result := time.Date(now.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, jst)
	if result.Before(now.Add(-90 * 24 * time.Hour)) {
		result = result.AddDate(1, 0, 0)
	}

	return &result, nil
}

func (s *ScraperService) extractMemberFromOnClick(onclick string) string {
	startMarker := "event_category':'"
	startIdx := strings.Index(onclick, startMarker)
	if startIdx == -1 {
		startMarker = `event_category":"`
		startIdx = strings.Index(onclick, startMarker)
	}

	if startIdx == -1 {
		return ""
	}

	startIdx += len(startMarker)
	endIdx := strings.Index(onclick[startIdx:], "'")
	if endIdx == -1 {
		endIdx = strings.Index(onclick[startIdx:], `"`)
	}

	if endIdx == -1 {
		return ""
	}

	return onclick[startIdx : startIdx+endIdx]
}

func (s *ScraperService) ValidateStructure(ctx context.Context) error {
	_, err := s.fetchAllStreams(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch streams for structure validation: %w", err)
	}
	return nil
}

type StructureChangedError struct {
	Message     string
	ParseErrors int
}

func (e *StructureChangedError) Error() string {
	return fmt.Sprintf("%s (parse errors: %d)", e.Message, e.ParseErrors)
}

func IsStructureError(err error) bool {
	structureChangedError := &StructureChangedError{}
	ok := errors.As(err, &structureChangedError)
	return ok
}

// Holodex API 폴백 시 YouTube 스크래퍼 데이터 활용
func (s *ScraperService) GetRecentVideos(ctx context.Context, channelID string, maxResults int) ([]*scraper.Video, error) {
	if s.youtubeScraper == nil {
		return nil, fmt.Errorf("youtube scraper not initialized")
	}

	videos, err := s.youtubeScraper.GetRecentVideos(ctx, channelID, maxResults)
	if err != nil {
		return nil, fmt.Errorf("youtube recent videos scraper error: %w", err)
	}

	s.logger.Debug("Recent videos fetched via scraper",
		slog.String("channel", channelID),
		slog.Int("count", len(videos)))

	return videos, nil
}

// Holodex API 폴백 시 YouTube 스크래퍼 데이터 활용
func (s *ScraperService) GetChannelStats(ctx context.Context, channelID string) (*scraper.ChannelStats, error) {
	if s.youtubeScraper == nil {
		return nil, fmt.Errorf("youtube scraper not initialized")
	}

	stats, err := s.youtubeScraper.GetChannelStats(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("youtube channel stats scraper error: %w", err)
	}

	s.logger.Debug("Channel stats fetched via scraper",
		slog.String("channel", channelID),
		slog.Int64("subscribers", stats.SubscriberCount))

	return stats, nil
}

func (s *ScraperService) GetChannelSnippet(ctx context.Context, channelID string) (*scraper.ChannelSnippet, error) {
	if s.youtubeScraper == nil {
		return nil, fmt.Errorf("youtube scraper not initialized")
	}

	snippet, err := s.youtubeScraper.GetChannelSnippet(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("youtube channel snippet scraper error: %w", err)
	}

	s.logger.Debug("Channel snippet fetched via scraper",
		slog.String("channel", channelID),
		slog.Int("avatars", len(snippet.Avatar)),
		slog.Int("banners", len(snippet.Banner)))

	return snippet, nil
}

func (s *ScraperService) GetPopularVideos(ctx context.Context, channelID string, maxResults int) ([]*scraper.Video, error) {
	if s.youtubeScraper == nil {
		return nil, fmt.Errorf("youtube scraper not initialized")
	}

	videos, err := s.youtubeScraper.GetPopularVideos(ctx, channelID, maxResults)
	if err != nil {
		return nil, fmt.Errorf("youtube popular videos scraper error: %w", err)
	}

	s.logger.Debug("Popular videos fetched via scraper",
		slog.String("channel", channelID),
		slog.Int("count", len(videos)))

	return videos, nil
}
