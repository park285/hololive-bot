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

package formatter

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/shared-go/pkg/stringutil"
)

type AlarmListEntry struct {
	MemberName string
	AlarmTypes domain.AlarmTypes
	NextStream *domain.NextStreamInfo
}

type alarmAddedTemplateData struct {
	MemberName string
	Added      bool
	NextStream *nextStreamInfoView
	Prefix     string
}

type alarmRemovedTemplateData struct {
	MemberName string
	Removed    bool
}

type alarmListTemplateData struct {
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
	Count int
}

type alarmNotificationTemplateData struct {
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

func (f *ResponseFormatter) alarmChannelName(ctx context.Context, notification *domain.AlarmNotification) string {
	if notification == nil {
		return ""
	}

	name := alarmBaseChannelName(notification)
	if name == "" {
		return ""
	}

	return f.alarmChannelNameWithOrg(ctx, name, notification.Channel)
}

func alarmBaseChannelName(notification *domain.AlarmNotification) string {
	if notification.Channel != nil {
		if name := stringutil.TrimSpace(notification.Channel.GetDisplayName()); name != "" {
			return name
		}
	}

	if notification.Stream == nil {
		return ""
	}

	return stringutil.TrimSpace(notification.Stream.ChannelName)
}

func (f *ResponseFormatter) alarmChannelNameWithOrg(ctx context.Context, name string, channel *domain.Channel) string {
	if channel == nil || channel.Org == nil {
		return name
	}

	displayOrg := f.formatStreamOrg(ctx, *channel.Org)
	if displayOrg == "" {
		return name
	}

	return fmt.Sprintf("[%s] %s", displayOrg, name)
}

func (f *ResponseFormatter) FormatAlarmAdded(ctx context.Context, memberName string, added bool, nextStreamInfo *domain.NextStreamInfo) string {
	data := alarmAddedTemplateData{
		MemberName: memberName,
		Added:      added,
		NextStream: f.buildNextStreamInfoView(ctx, nextStreamInfo),
		Prefix:     f.prefix,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdAlarmAdded, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}

func (f *ResponseFormatter) FormatAlarmRemoved(ctx context.Context, memberName string, removed bool) string {
	data := alarmRemovedTemplateData{
		MemberName: memberName,
		Removed:    removed,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdAlarmRemoved, data)
	if err != nil {
		return messagestrings.FallbackSentinel
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

func (f *ResponseFormatter) buildNextStreamInfoView(ctx context.Context, info *domain.NextStreamInfo) *nextStreamInfoView {
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
		if !f.populateUpcomingNextStreamView(ctx, view, info) {
			return nil
		}
	}

	return view
}

func (f *ResponseFormatter) populateUpcomingNextStreamView(ctx context.Context, view *nextStreamInfoView, info *domain.NextStreamInfo) bool {
	if info.StartScheduled == nil || view.URL == "" {
		return false
	}

	scheduled := *info.StartScheduled
	view.ScheduledKST = util.FormatKST(scheduled, "01/02 15:04")

	timeLeft := time.Until(scheduled)
	if timeLeft <= 0 {
		view.StartingSoon = true
		return true
	}

	view.TimeDetail = f.formatUpcomingTimeDetail(ctx, timeLeft)
	return true
}

func (f *ResponseFormatter) formatUpcomingTimeDetail(ctx context.Context, timeLeft time.Duration) string {
	if timeLeft <= 0 {
		return ""
	}

	hoursLeft := int(timeLeft.Hours())
	minutesLeft := int(timeLeft.Minutes()) % 60

	switch {
	case hoursLeft >= 24:
		return fmt.Sprintf(f.messageStrings.GetOrContext(ctx, messagestrings.NamespaceTimeFmt, "relative_days", "%d일 후"), hoursLeft/24)
	case hoursLeft > 0:
		return fmt.Sprintf(f.messageStrings.GetOrContext(ctx, messagestrings.NamespaceTimeFmt, "relative_hours_minutes", "%d시간 %d분 후"), hoursLeft, minutesLeft)
	default:
		return fmt.Sprintf(f.messageStrings.GetOrContext(ctx, messagestrings.NamespaceTimeFmt, "relative_minutes", "%d분 후"), int(timeLeft.Minutes()))
	}
}
