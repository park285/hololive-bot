package chzzk

import (
	"fmt"
	"time"
)

const ChzzkTimeLayout = "2006-01-02 15:04:05"

type LiveStatusResponse struct {
	Code    int                `json:"code"`
	Content *LiveStatusContent `json:"content"`
}

type LiveStatusContent struct {
	LiveTitle           string `json:"liveTitle"`
	Status              string `json:"status"`
	ConcurrentUserCount int    `json:"concurrentUserCount"`
	LiveCategoryValue   string `json:"liveCategoryValue"`
	ChatChannelId       string `json:"chatChannelId"`
}

type ScheduledLivesResponse struct {
	Code    int                    `json:"code"`
	Content *ScheduledLivesContent `json:"content"`
}

type ScheduledLivesContent struct {
	ScheduledLives []ScheduledLive `json:"scheduledLives"`
}

type ScheduledLive struct {
	LiveId           int    `json:"liveId"`
	LiveTitle        string `json:"liveTitle"`
	ScheduledStartAt string `json:"scheduledStartAt"`
}

func ParseScheduledStartAt(s string) (time.Time, error) {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to load Asia/Seoul location: %w", err)
	}

	parsed, err := time.ParseInLocation(ChzzkTimeLayout, s, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse time %q with layout %q: %w", s, ChzzkTimeLayout, err)
	}

	return parsed, nil
}
