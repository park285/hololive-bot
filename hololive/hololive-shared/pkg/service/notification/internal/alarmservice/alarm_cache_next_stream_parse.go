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

package alarmservice

import (
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/shared-go/pkg/stringutil"
)

func (as *AlarmService) parseNextStreamInfo(channelID string, data map[string]string) *domain.NextStreamInfo {
	if len(data) == 0 {
		return nil
	}

	info := parseCachedNextStreamInfo(data)
	if !as.hasValidNextStreamStatus(channelID, info.Status) {
		return nil
	}

	startScheduledStr := stringutil.TrimSpace(data["start_scheduled"])
	if !as.parseNextStreamStart(channelID, startScheduledStr, info) {
		return nil
	}

	if !as.hasCompleteUpcomingStreamInfo(channelID, startScheduledStr, info) {
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

func (as *AlarmService) hasValidNextStreamStatus(channelID string, status domain.NextStreamStatus) bool {
	if status.IsValid() {
		return true
	}

	as.logger.Warn("Unexpected cache status",
		slog.String("channel_id", channelID),
		slog.String("status", status.String()),
	)

	return false
}

func (as *AlarmService) parseNextStreamStart(channelID, startScheduledStr string, info *domain.NextStreamInfo) bool {
	if startScheduledStr == "" {
		return true
	}

	scheduledDate, err := time.Parse(time.RFC3339, startScheduledStr)
	if err != nil {
		as.logger.Error("Failed to parse scheduled time",
			slog.String("channel_id", channelID),
			slog.String("start_scheduled", startScheduledStr),
			slog.Any("error", err),
		)

		return false
	}

	info.StartScheduled = &scheduledDate

	return true
}

func (as *AlarmService) hasCompleteUpcomingStreamInfo(
	channelID, startScheduledStr string,
	info *domain.NextStreamInfo,
) bool {
	if !info.Status.IsUpcoming() {
		return true
	}

	if startScheduledStr != "" && info.Title != "" && info.VideoID != "" && info.StartScheduled != nil {
		return true
	}

	as.logger.Error("Incomplete cache data for upcoming stream",
		slog.String("channel_id", channelID),
		slog.Bool("has_title", info.Title != ""),
		slog.Bool("has_start", startScheduledStr != ""),
		slog.Bool("has_video_id", info.VideoID != ""),
	)

	return false
}
