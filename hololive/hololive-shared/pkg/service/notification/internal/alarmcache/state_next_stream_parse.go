package alarmcache

import (
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/shared-go/pkg/stringutil"
)

func (s *State) ParseNextStreamInfo(channelID string, data map[string]string) *domain.NextStreamInfo {
	if len(data) == 0 {
		return nil
	}

	info := parseCachedNextStreamInfo(data)
	if !s.hasValidNextStreamStatus(channelID, info.Status) {
		return nil
	}

	startScheduledStr := stringutil.TrimSpace(data["start_scheduled"])
	if !s.parseNextStreamStart(channelID, startScheduledStr, info) {
		return nil
	}

	if !s.hasCompleteUpcomingStreamInfo(channelID, startScheduledStr, info) {
		return nil
	}

	return info
}

func parseCachedNextStreamInfo(data map[string]string) *domain.NextStreamInfo {
	return &domain.NextStreamInfo{
		Status:  domain.NextStreamStatus(stringutil.TrimSpace(data["status"])),
		VideoID: stringutil.TrimSpace(data["video_id"]),
		Title:   stringutil.TrimSpace(data["title"]),
	}
}

func (s *State) hasValidNextStreamStatus(channelID string, status domain.NextStreamStatus) bool {
	if status.IsValid() {
		return true
	}

	s.Logger.Warn("Unexpected cache status",
		slog.String("channel_id", channelID),
		slog.String("status", status.String()),
	)

	return false
}

func (s *State) parseNextStreamStart(channelID, startScheduledStr string, info *domain.NextStreamInfo) bool {
	if startScheduledStr == "" {
		return true
	}

	scheduledDate, err := time.Parse(time.RFC3339, startScheduledStr)
	if err != nil {
		s.Logger.Error("Failed to parse scheduled time",
			slog.String("channel_id", channelID),
			slog.String("start_scheduled", startScheduledStr),
			slog.Any("error", err),
		)

		return false
	}

	info.StartScheduled = &scheduledDate

	return true
}

func (s *State) hasCompleteUpcomingStreamInfo(
	channelID, startScheduledStr string,
	info *domain.NextStreamInfo,
) bool {
	if !info.Status.IsUpcoming() {
		return true
	}

	if startScheduledStr != "" && info.Title != "" && info.VideoID != "" && info.StartScheduled != nil {
		return true
	}

	s.Logger.Error("Incomplete cache data for upcoming stream",
		slog.String("channel_id", channelID),
		slog.Bool("has_title", info.Title != ""),
		slog.Bool("has_start", startScheduledStr != ""),
		slog.Bool("has_video_id", info.VideoID != ""),
	)

	return false
}
