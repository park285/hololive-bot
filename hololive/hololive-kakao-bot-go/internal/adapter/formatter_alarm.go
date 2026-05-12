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

package adapter

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

type AlarmListEntry struct {
	MemberName string
	AlarmTypes domain.AlarmTypes
	NextStream *domain.NextStreamInfo
}

type alarmAddedTemplateData struct {
	Emoji      UIEmoji
	MemberName string
	Added      bool
	NextStream *nextStreamInfoView
	Prefix     string
}

type alarmRemovedTemplateData struct {
	Emoji      UIEmoji
	MemberName string
	Removed    bool
}

type alarmListTemplateData struct {
	Emoji  UIEmoji
	Count  int
	Prefix string
	Alarms []alarmListEntryView
}

type alarmListEntryView struct {
	MemberName string
	TypesLabel string
	NextStream *nextStreamInfoView
}

type nextStreamInfoView struct {
	Status       string
	Title        string
	URL          string
	ScheduledKST string
	TimeDetail   string
	StartingSoon bool
}

type alarmClearedTemplateData struct {
	Emoji UIEmoji
	Count int
}

type alarmNotificationTemplateData struct {
	Emoji            UIEmoji
	ChannelName      string
	MinutesUntil     int
	Title            string
	URL              string
	ScheduleMessage  string
	ScheduledTimeKST string // "21:00" 형식, MinutesUntil > 0 && StartScheduled != nil 일 때만 세팅
}

type alarmNotificationGroupEntry struct {
	ChannelName  string
	Title        string
	URL          string
	ScheduledKST string // "21:00" 형식
}

func alarmChannelName(notification *domain.AlarmNotification) string {
	if notification == nil {
		return ""
	}

	var name string

	if notification.Channel != nil {
		name = stringutil.TrimSpace(notification.Channel.GetDisplayName())
	}

	if name == "" && notification.Stream != nil {
		name = stringutil.TrimSpace(notification.Stream.ChannelName)
	}

	if name == "" {
		return ""
	}

	// org 태그 추가 (Hololive 제외)
	if notification.Channel != nil && notification.Channel.Org != nil {
		org := *notification.Channel.Org
		if org != "" && org != "Hololive" {
			displayOrg := org
			switch org {
			case "Nijisanji":
				displayOrg = "니지산지"
			case "VSPO":
				displayOrg = "VSPO"
			case "Independents":
				displayOrg = "개인세"
			case "Stellive":
				displayOrg = "스텔라이브"
			}

			name = fmt.Sprintf("[%s] %s", displayOrg, name)
		}
	}

	return name
}

func (f *ResponseFormatter) FormatAlarmAdded(ctx context.Context, memberName string, added bool, nextStreamInfo *domain.NextStreamInfo) string {
	data := alarmAddedTemplateData{
		Emoji:      DefaultEmoji,
		MemberName: memberName,
		Added:      added,
		NextStream: buildNextStreamInfoView(nextStreamInfo),
		Prefix:     f.prefix,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdAlarmAdded, data)
	if err != nil {
		return ErrorMessage(ErrDisplayAlarmAddFailed)
	}

	return rendered
}

func (f *ResponseFormatter) FormatAlarmRemoved(ctx context.Context, memberName string, removed bool) string {
	data := alarmRemovedTemplateData{
		Emoji:      DefaultEmoji,
		MemberName: memberName,
		Removed:    removed,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdAlarmRemoved, data)
	if err != nil {
		return ErrorMessage(ErrDisplayAlarmRemoveFailed)
	}

	return rendered
}

const youtubeWatchURLPrefix = "https://youtube.com/watch?v="

func summarizeNextStreamInfo(info *domain.NextStreamInfo) *domain.NextStreamInfo {
	if info == nil || !info.Status.IsLive() {
		return nil
	}

	return info
}

func buildNextStreamInfoView(info *domain.NextStreamInfo) *nextStreamInfoView {
	if info == nil || !info.Status.IsValid() {
		return nil
	}

	view := &nextStreamInfoView{
		Status: info.Status.String(),
	}

	if title := stringutil.TrimSpace(info.Title); title != "" {
		view.Title = stringutil.TruncateString(title, constants.StringLimits.NextStreamTitle)
	}

	if videoID := stringutil.TrimSpace(info.VideoID); videoID != "" {
		view.URL = youtubeWatchURLPrefix + videoID
	}

	if info.Status.IsUpcoming() {
		if info.StartScheduled == nil || view.URL == "" {
			return nil
		}

		scheduled := *info.StartScheduled

		view.ScheduledKST = util.FormatKST(scheduled, "01/02 15:04")

		timeLeft := time.Until(scheduled)
		if timeLeft <= 0 {
			view.StartingSoon = true
		} else {
			view.TimeDetail = formatUpcomingTimeDetail(timeLeft)
		}
	}

	return view
}

func formatUpcomingTimeDetail(timeLeft time.Duration) string {
	if timeLeft <= 0 {
		return ""
	}

	hoursLeft := int(timeLeft.Hours())
	minutesLeft := int(timeLeft.Minutes()) % 60

	switch {
	case hoursLeft >= 24:
		return fmt.Sprintf("%d일 후", hoursLeft/24)
	case hoursLeft > 0:
		return fmt.Sprintf("%d시간 %d분 후", hoursLeft, minutesLeft)
	default:
		return fmt.Sprintf("%d분 후", int(timeLeft.Minutes()))
	}
}
