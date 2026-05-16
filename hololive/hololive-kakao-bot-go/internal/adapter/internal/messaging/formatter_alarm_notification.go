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

package messaging

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

func (f *ResponseFormatter) AlarmNotification(ctx context.Context, notification *domain.AlarmNotification) string {
	if notification == nil || notification.Stream == nil {
		return ""
	}

	channelName := alarmChannelName(notification)
	stream := notification.Stream
	data := alarmNotificationTemplateData{
		Emoji:            DefaultEmoji,
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
		return alarmNotificationRenderFallback(templateKey, data)
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

func alarmNotificationRenderFallback(templateKey domain.TemplateKey, data alarmNotificationTemplateData) string {
	if templateKey == domain.TemplateKeyCmdAlarmLiveStarted {
		return fmt.Sprintf("🔴 %s 방송 시작됨\n📺 %s\n🔗 %s", data.ChannelName, data.Title, data.URL)
	}

	return ErrorMessage(ErrDisplayAlarmNotifyFailed)
}

func (f *ResponseFormatter) AlarmNotificationGroup(minutesUntil int, notifications []*domain.AlarmNotification) string {
	if len(notifications) == 0 {
		return ""
	}

	entries := alarmNotificationGroupEntries(notifications)
	if len(entries) == 0 {
		return ""
	}

	sortAlarmNotificationGroupEntries(entries)
	return renderAlarmNotificationGroup(minutesUntil, entries)
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
	slices.SortStableFunc(entries, func(a, b alarmNotificationGroupEntry) int {
		return compareAlarmNotificationGroupEntry(a, b)
	})
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

func renderAlarmNotificationGroup(minutesUntil int, entries []alarmNotificationGroupEntry) string {
	instruction := DefaultEmoji.Alarm + " 방송 알림"

	var sb strings.Builder
	sb.WriteString(CountedHeader(DefaultEmoji.Alarm, "방송 알림", len(entries)))
	sb.WriteString("\n\n")
	sb.WriteString(groupAlarmSummaryLine(minutesUntil, entries))
	sb.WriteString("\n\n")

	for idx, entry := range entries {
		writeAlarmNotificationGroupEntry(&sb, idx, len(entries), minutesUntil, entry)
	}

	content := stringutil.TrimSpace(sb.String())
	if content == "" {
		return ""
	}

	return util.ApplyKakaoSeeMorePadding(content, instruction)
}

func writeAlarmNotificationGroupEntry(sb *strings.Builder, idx, count, minutesUntil int, entry alarmNotificationGroupEntry) {
	name := stringutil.TrimSpace(entry.ChannelName)
	if name == "" {
		name = "알 수 없는 채널"
	}

	sb.WriteString(groupAlarmEntryLine(idx+1, name, entry.ScheduledKST, minutesUntil))
	sb.WriteByte('\n')

	if entry.Title != "" {
		fmt.Fprintf(sb, "   %s\n", entry.Title)
	}
	if entry.URL != "" {
		fmt.Fprintf(sb, "   %s\n", entry.URL)
	}
	if idx < count-1 {
		sb.WriteString("\n")
	}
}

func groupAlarmEntryLine(index int, channelName, scheduledKST string, minutesUntil int) string {
	if scheduledKST != "" {
		if minutesUntil > 0 {
			return fmt.Sprintf("%d. %s (%s 방송예정)", index, channelName, scheduledKST)
		}

		return fmt.Sprintf("%d. %s (%s 방송 시작)", index, channelName, scheduledKST)
	}

	if minutesUntil > 0 {
		return fmt.Sprintf("%d. %s (방송예정)", index, channelName)
	}

	return fmt.Sprintf("%d. %s", index, channelName)
}

func groupAlarmSummaryLine(minutesUntil int, entries []alarmNotificationGroupEntry) string {
	if minutesUntil <= 0 {
		return DefaultEmoji.Time + " 여러 방송이 시작되었습니다."
	}

	seen := make(map[string]struct{}, len(entries))

	scheduledTimes := make([]string, 0, len(entries))
	for _, entry := range entries {
		scheduledKST := stringutil.TrimSpace(entry.ScheduledKST)
		if scheduledKST == "" {
			continue
		}

		if _, ok := seen[scheduledKST]; ok {
			continue
		}

		seen[scheduledKST] = struct{}{}
		scheduledTimes = append(scheduledTimes, scheduledKST)
	}

	if len(scheduledTimes) == 0 {
		return DefaultEmoji.Time + " 여러 방송이 곧 시작됩니다."
	}

	slices.Sort(scheduledTimes)

	if len(scheduledTimes) == 1 {
		return fmt.Sprintf("%s %s 방송예정", DefaultEmoji.Time, scheduledTimes[0])
	}

	return fmt.Sprintf("%s 방송예정: %s", DefaultEmoji.Time, strings.Join(scheduledTimes, ", "))
}
