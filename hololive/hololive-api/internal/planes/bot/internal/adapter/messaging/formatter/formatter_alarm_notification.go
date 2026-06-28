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
	"slices"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/shared-go/pkg/stringutil"
)

func (f *ResponseFormatter) AlarmNotification(ctx context.Context, notification *domain.AlarmNotification) string {
	if notification == nil || notification.Stream == nil {
		return ""
	}

	channelName := alarmChannelName(notification)
	stream := notification.Stream
	data := alarmNotificationTemplateData{
		ChannelName:      channelName,
		MinutesUntil:     notification.MinutesUntil,
		Title:            stringutil.TruncateString(stream.Title, constants.StringLimits.StreamTitle),
		URL:              alarmNotificationURLText(stream),
		ScheduleMessage:  notification.ScheduleChangeMessage,
		ScheduledTimeKST: alarmNotificationScheduledKST(stream),
	}

	templateKey := alarmNotificationTemplateKey(notification.MinutesUntil)

	rendered, err := f.render(ctx, templateKey, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}

func alarmNotificationURLText(stream *domain.Stream) string {
	if stream.IsTwitchOnly {
		return fmt.Sprintf("📺 Twitch: %s", stream.GetTwitchLiveURL())
	}
	if isIntegratedYouTubeChzzkStream(stream) {
		return fmt.Sprintf("📺 YouTube: %s\n📺 치지직: %s", stream.GetYouTubeURL(), stream.GetChzzkLiveURL())
	}
	if isChzzkOnlyAlarmStream(stream) {
		return fmt.Sprintf("📺 치지직: %s", stream.GetChzzkLiveURL())
	}

	return stream.GetYouTubeURL()
}

func isIntegratedYouTubeChzzkStream(stream *domain.Stream) bool {
	return stream.IsIntegrated && stream.HasYouTubeInfo() && stream.ChzzkChannelID != ""
}

func isChzzkOnlyAlarmStream(stream *domain.Stream) bool {
	return stream.IsChzzkOnly || (!stream.HasYouTubeInfo() && stream.ChzzkChannelID != "")
}

func alarmNotificationScheduledKST(stream *domain.Stream) string {
	if stream.StartScheduled == nil {
		return ""
	}

	return util.FormatKST(*stream.StartScheduled, "15:04")
}

func alarmNotificationTemplateKey(minutesUntil int) domain.TemplateKey {
	if minutesUntil <= 0 {
		return domain.TemplateKeyCmdAlarmLiveStarted
	}

	return domain.TemplateKeyCmdAlarmNotification
}

type alarmGroupEntryView struct {
	Index        int
	ChannelName  string
	ScheduledKST string
	Title        string
	URL          string
}

type alarmGroupTemplateData struct {
	Count          int
	MinutesUntil   int
	ScheduledTimes []string
	Entries        []alarmGroupEntryView
}

func (f *ResponseFormatter) AlarmNotificationGroup(ctx context.Context, minutesUntil int, notifications []*domain.AlarmNotification) string {
	if len(notifications) == 0 {
		return ""
	}

	entries := alarmNotificationGroupEntries(notifications)
	if len(entries) == 0 {
		return ""
	}

	sortAlarmNotificationGroupEntries(entries)

	data := alarmGroupTemplateData{
		Count:          len(entries),
		MinutesUntil:   minutesUntil,
		ScheduledTimes: alarmGroupScheduledTimes(entries),
		Entries:        alarmGroupEntryViews(entries),
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdAlarmNotificationGroup, data)
	if err != nil {
		return messagestrings.FallbackSentinel
	}

	return rendered
}

func alarmGroupScheduledTimes(entries []alarmNotificationGroupEntry) []string {
	seen := make(map[string]struct{}, len(entries))
	times := make([]string, 0, len(entries))

	for _, entry := range entries {
		scheduledKST := stringutil.TrimSpace(entry.ScheduledKST)
		if scheduledKST == "" {
			continue
		}

		if _, ok := seen[scheduledKST]; ok {
			continue
		}

		seen[scheduledKST] = struct{}{}
		times = append(times, scheduledKST)
	}

	slices.Sort(times)
	return times
}

func alarmGroupEntryViews(entries []alarmNotificationGroupEntry) []alarmGroupEntryView {
	views := make([]alarmGroupEntryView, 0, len(entries))
	for idx, entry := range entries {
		views = append(views, alarmGroupEntryView{
			Index:        idx + 1,
			ChannelName:  entry.ChannelName,
			ScheduledKST: entry.ScheduledKST,
			Title:        entry.Title,
			URL:          entry.URL,
		})
	}

	return views
}

func alarmNotificationGroupEntries(notifications []*domain.AlarmNotification) []alarmNotificationGroupEntry {
	entries := make([]alarmNotificationGroupEntry, 0, len(notifications))
	for _, notification := range notifications {
		if notification == nil || notification.Stream == nil {
			continue
		}

		var scheduledKST string

		if notification.Stream.StartScheduled != nil {
			scheduledKST = util.FormatKST(*notification.Stream.StartScheduled, "15:04")
		}

		entries = append(entries, alarmNotificationGroupEntry{
			ChannelName:  alarmChannelName(notification),
			Title:        stringutil.TruncateString(stringutil.TrimSpace(notification.Stream.Title), constants.StringLimits.StreamTitle),
			URL:          stringutil.TrimSpace(notification.Stream.GetYouTubeURL()),
			ScheduledKST: scheduledKST,
		})
	}

	return entries
}

func sortAlarmNotificationGroupEntries(entries []alarmNotificationGroupEntry) {
	slices.SortStableFunc(entries, compareAlarmNotificationGroupEntry)
}

func compareAlarmNotificationGroupEntry(a, b alarmNotificationGroupEntry) int {
	if a.ChannelName < b.ChannelName {
		return -1
	}
	if a.ChannelName > b.ChannelName {
		return 1
	}
	if a.Title < b.Title {
		return -1
	}
	if a.Title > b.Title {
		return 1
	}
	return 0
}
