package htmlscraper

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

var officialScheduleJST = time.FixedZone("Asia/Tokyo", 9*60*60)

type officialScheduleVideoItem struct {
	Datetime  string `json:"datetime"`
	IsLive    bool   `json:"isLive"`
	URL       string `json:"url"`
	Thumbnail string `json:"thumbnail"`
	Title     string `json:"title"`
	Name      string `json:"name"`
	Talent    struct {
		Name string `json:"name"`
	} `json:"talent"`
}

type officialScheduleMappedRow struct {
	stream           *domain.Stream
	hasProviderTitle bool
	hasProviderThumb bool
}

type officialScheduleRowStats struct {
	Total     int
	Valid     int
	Invalid   int
	Unmapped  int
	Duplicate int
}

func (s *Service) mapOfficialScheduleRows(rows []jsontext.Value) ([]*domain.Stream, officialScheduleRowStats, error) {
	stats := officialScheduleRowStats{Total: len(rows)}
	streams := make([]*domain.Stream, 0, len(rows))
	byVideoID := make(map[string]*officialScheduleMappedRow, len(rows))

	for _, rawRow := range rows {
		s.applyOfficialScheduleRow(rawRow, &streams, byVideoID, &stats)
	}
	if len(rows) > 0 && len(streams) == 0 {
		return nil, stats, &StructureChangedError{
			Message:     "official schedule API returned no valid video rows",
			InvalidRows: stats.Invalid,
		}
	}
	return streams, stats, nil
}

func (s *Service) applyOfficialScheduleRow(
	rawRow jsontext.Value,
	streams *[]*domain.Stream,
	byVideoID map[string]*officialScheduleMappedRow,
	stats *officialScheduleRowStats,
) {
	mapped, err := s.mapOfficialScheduleRow(rawRow)
	if err != nil {
		stats.Invalid++
		return
	}
	if existing := byVideoID[mapped.stream.ID]; existing != nil {
		mergeOfficialScheduleRows(existing, mapped)
		stats.Duplicate++
		return
	}

	byVideoID[mapped.stream.ID] = mapped
	*streams = append(*streams, mapped.stream)
	if mapped.stream.ChannelID == "" {
		stats.Unmapped++
		return
	}
	stats.Valid++
}

func (s *Service) mapOfficialScheduleRow(rawRow jsontext.Value) (*officialScheduleMappedRow, error) {
	var item officialScheduleVideoItem
	if err := jsonv2.Unmarshal(rawRow, &item); err != nil {
		return nil, fmt.Errorf("decode official schedule video row: %w", err)
	}

	videoID, link, err := parseOfficialScheduleVideoURL(item.URL)
	if err != nil {
		return nil, err
	}
	startTime, err := time.ParseInLocation("2006/01/02 15:04:05", stringutil.TrimSpace(item.Datetime), officialScheduleJST)
	if err != nil {
		return nil, fmt.Errorf("parse official schedule datetime: %w", err)
	}

	channelName := stringutil.TrimSpace(item.Name)
	if channelName == "" {
		channelName = stringutil.TrimSpace(item.Talent.Name)
	}
	if channelName == "" {
		return nil, fmt.Errorf("official schedule video row has no talent name")
	}

	providerTitle := stringutil.TrimSpace(item.Title)
	title := providerTitle
	if title == "" {
		title = channelName
	}
	thumbnail, providerThumbnail := officialScheduleThumbnail(item.Thumbnail, videoID)

	return &officialScheduleMappedRow{
		stream: &domain.Stream{
			ID:             videoID,
			Title:          title,
			ChannelID:      s.identityIndex.Resolve(channelName),
			ChannelName:    channelName,
			Status:         domain.StreamStatusUpcoming,
			StartScheduled: &startTime,
			Thumbnail:      &thumbnail,
			Link:           &link,
		},
		hasProviderTitle: providerTitle != "",
		hasProviderThumb: providerThumbnail,
	}, nil
}

func parseOfficialScheduleVideoURL(rawURL string) (parsedVideoID, parsedURL string, parseErr error) {
	parsed, err := url.Parse(stringutil.TrimSpace(rawURL))
	if err != nil {
		return "", "", fmt.Errorf("parse official schedule video URL: %w", err)
	}
	host := strings.ToLower(parsed.Hostname())
	if parsed.Scheme != "https" || (host != "youtube.com" && host != "www.youtube.com") || parsed.Path != "/watch" {
		return "", "", fmt.Errorf("unsupported official schedule video URL")
	}

	videoID := stringutil.TrimSpace(parsed.Query().Get("v"))
	if !validOfficialScheduleVideoID(videoID) {
		return "", "", fmt.Errorf("invalid official schedule YouTube video ID")
	}
	canonical := "https://www.youtube.com/watch?v=" + videoID
	return videoID, canonical, nil
}

func validOfficialScheduleVideoID(videoID string) bool {
	if videoID == "" || len(videoID) > 128 {
		return false
	}
	for _, char := range videoID {
		if !isOfficialScheduleVideoIDChar(char) {
			return false
		}
	}
	return true
}

func isOfficialScheduleVideoIDChar(char rune) bool {
	return char >= 'a' && char <= 'z' ||
		char >= 'A' && char <= 'Z' ||
		char >= '0' && char <= '9' ||
		char == '-' || char == '_'
}

func officialScheduleThumbnail(rawURL, videoID string) (string, bool) {
	thumbnail := stringutil.TrimSpace(rawURL)
	parsed, err := url.Parse(thumbnail)
	if err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil {
		return thumbnail, true
	}
	return fmt.Sprintf("https://img.youtube.com/vi/%s/maxresdefault.jpg", videoID), false
}

func mergeOfficialScheduleRows(existing, candidate *officialScheduleMappedRow) {
	if existing == nil || candidate == nil || existing.stream == nil || candidate.stream == nil {
		return
	}
	if !existing.hasProviderTitle && candidate.hasProviderTitle {
		existing.stream.Title = candidate.stream.Title
		existing.hasProviderTitle = true
	}
	if !existing.hasProviderThumb && candidate.hasProviderThumb {
		existing.stream.Thumbnail = candidate.stream.Thumbnail
		existing.hasProviderThumb = true
	}
}

func (s *Service) convertEventToStream(event *parser.UpcomingEvent, channelID string) *domain.Stream {
	stream := &domain.Stream{
		ID:          event.VideoID,
		Title:       event.Title,
		ChannelID:   channelID,
		ChannelName: event.ChannelTitle,
		Status:      s.mapEventStatus(event.Status),
	}

	if len(event.Thumbnail) > 0 {
		bestThumb := event.Thumbnail[len(event.Thumbnail)-1].URL
		stream.Thumbnail = &bestThumb
	} else {
		thumbURL := fmt.Sprintf("https://i.ytimg.com/vi/%s/maxresdefault.jpg", event.VideoID)
		stream.Thumbnail = &thumbURL
	}

	linkURL := fmt.Sprintf("https://www.youtube.com/watch?v=%s", event.VideoID)
	stream.Link = &linkURL
	if event.StartTime != nil {
		startTime := time.Unix(*event.StartTime, 0)
		stream.StartScheduled = &startTime
	}
	return stream
}

func (s *Service) mapEventStatus(status string) domain.StreamStatus {
	switch status {
	case "LIVE":
		return domain.StreamStatusLive
	case "UPCOMING":
		return domain.StreamStatusUpcoming
	default:
		return domain.StreamStatusUpcoming
	}
}
