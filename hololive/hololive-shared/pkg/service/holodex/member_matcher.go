package holodex

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func buildMemberNameMap(membersData domain.MemberDataProvider) map[string]string {
	nameMap := make(map[string]string)
	if membersData == nil {
		return nameMap
	}
	for _, member := range membersData.GetAllMembers() {
		addMemberNames(nameMap, member)
	}
	return nameMap
}

func addMemberNames(nameMap map[string]string, member *domain.Member) {
	nameMap[stringutil.Normalize(member.Name)] = member.ChannelID
	if member.NameJa != "" {
		nameMap[stringutil.Normalize(member.NameJa)] = member.ChannelID
	}
	if member.Aliases == nil {
		return
	}
	addMemberAliases(nameMap, member.Aliases.Ko, member.ChannelID)
	addMemberAliases(nameMap, member.Aliases.Ja, member.ChannelID)
}

func addMemberAliases(nameMap map[string]string, aliases []string, channelID string) {
	for _, alias := range aliases {
		nameMap[stringutil.Normalize(alias)] = channelID
	}
}

func (s *ScraperService) parseStreamElement(sel *goquery.Selection, currentDate string) (*domain.Stream, error) {
	videoURL, videoID, err := s.parseStreamVideo(sel)
	if err != nil {
		return nil, err
	}

	timeText := stringutil.TrimSpace(sel.Find(".datetime").Text())
	memberName := s.extractMemberName(sel)
	startTime, err := s.parseDatetimeWithContext(currentDate, timeText)
	if err != nil {
		s.logger.Debug("Failed to parse datetime",
			slog.String("date", currentDate),
			slog.String("time", timeText),
			slog.Any("error", err))
	}

	channelID := s.matchMemberToChannel(memberName)
	if channelID == "" {
		s.logger.Debug("Could not match member name to channel ID",
			slog.String("member_name", memberName),
			slog.String("video_id", videoID))
	}

	return buildStream(videoID, videoURL, memberName, channelID, startTime), nil
}

func (s *ScraperService) parseStreamVideo(sel *goquery.Selection) (string, string, error) {
	videoURL, exists := sel.Attr("href")
	if !exists || !strings.Contains(videoURL, "youtube.com/watch?v=") {
		return "", "", fmt.Errorf("invalid video URL")
	}

	videoID := s.extractVideoID(videoURL)
	if videoID == "" {
		return "", "", fmt.Errorf("could not extract video ID from %s", videoURL)
	}
	return videoURL, videoID, nil
}

func (s *ScraperService) extractMemberName(sel *goquery.Selection) string {
	memberName := stringutil.TrimSpace(sel.Find(".name").Text())
	if memberName == "" {
		memberName = stringutil.TrimSpace(sel.Find(".text").Text())
	}

	if onclickStr, exists := sel.Attr("onclick"); exists {
		if extractedName := s.extractMemberFromOnClick(onclickStr); extractedName != "" {
			return extractedName
		}
	}
	return memberName
}

func buildStream(videoID, videoURL, memberName, channelID string, startTime *time.Time) *domain.Stream {
	thumbnailURL := fmt.Sprintf("https://img.youtube.com/vi/%s/maxresdefault.jpg", videoID)
	return &domain.Stream{
		ID:             videoID,
		Title:          memberName,
		ChannelID:      channelID,
		ChannelName:    memberName,
		Status:         domain.StreamStatusUpcoming,
		StartScheduled: startTime,
		Link:           &videoURL,
		Thumbnail:      &thumbnailURL,
	}
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
	jst, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		s.logger.Debug("Failed to load Asia/Tokyo timezone, using FixedZone", slog.Any("error", err))
		jst = time.FixedZone("JST", 9*60*60)
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
