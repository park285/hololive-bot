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
