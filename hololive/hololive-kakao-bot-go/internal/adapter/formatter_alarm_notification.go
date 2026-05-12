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

	var urlText string

	switch {
	case stream.IsTwitchOnly:
		urlText = fmt.Sprintf("📺 Twitch: %s", stream.GetTwitchLiveURL())
	case stream.IsIntegrated && stream.HasYouTubeInfo() && stream.ChzzkChannelID != "":
		urlText = fmt.Sprintf("📺 YouTube: %s\n📺 치지직: %s",
			stream.GetYouTubeURL(),
			stream.GetChzzkLiveURL())
	case stream.IsChzzkOnly || (!stream.HasYouTubeInfo() && stream.ChzzkChannelID != ""):
		urlText = fmt.Sprintf("📺 치지직: %s", stream.GetChzzkLiveURL())
	default:
		urlText = stream.GetYouTubeURL()
	}

	// 방송 시각 표시: StartScheduled가 있으면 MinutesUntil 값과 무관하게 절대 시각 표시
	var scheduledTimeKST string

	if notification.Stream.StartScheduled != nil {
		scheduledTimeKST = util.FormatKST(*notification.Stream.StartScheduled, "15:04")
	}

	data := alarmNotificationTemplateData{
		Emoji:            DefaultEmoji,
		ChannelName:      channelName,
		MinutesUntil:     notification.MinutesUntil,
		Title:            stringutil.TruncateString(notification.Stream.Title, constants.StringLimits.StreamTitle),
		URL:              urlText,
		ScheduleMessage:  notification.ScheduleChangeMessage,
		ScheduledTimeKST: scheduledTimeKST,
	}

	templateKey := domain.TemplateKeyCmdAlarmNotification

	if notification.MinutesUntil <= 0 {
		templateKey = domain.TemplateKeyCmdAlarmLiveStarted
	}

	rendered, err := f.render(ctx, templateKey, data)
	if err != nil {
		if templateKey == domain.TemplateKeyCmdAlarmLiveStarted {
			// 마이그레이션 적용 전에도 catch-up 문구가 "방송 시작"으로 보이도록 안전한 fallback.
			return fmt.Sprintf("🔴 %s 방송 시작됨\n📺 %s\n🔗 %s", channelName, data.Title, data.URL)
		}

		return ErrorMessage(ErrDisplayAlarmNotifyFailed)
	}

	// 단건 알림은 즉시성이 중요 → 패딩 미적용 (전체 보기 없이 바로 노출)
	return rendered
}

func (f *ResponseFormatter) AlarmNotificationGroup(minutesUntil int, notifications []*domain.AlarmNotification) string {
	if len(notifications) == 0 {
		return ""
	}

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

	if len(entries) == 0 {
		return ""
	}

	slices.SortStableFunc(entries, func(a, b alarmNotificationGroupEntry) int {
		if a.ChannelName != b.ChannelName {
			if a.ChannelName < b.ChannelName {
				return -1
			}

			return 1
		}

		if a.Title < b.Title {
			return -1
		}

		if a.Title > b.Title {
			return 1
		}

		return 0
	})

	instruction := DefaultEmoji.Alarm + " 방송 알림"

	var sb strings.Builder
	sb.WriteString(CountedHeader(DefaultEmoji.Alarm, "방송 알림", len(entries)))
	sb.WriteString("\n\n")
	sb.WriteString(groupAlarmSummaryLine(minutesUntil, entries))
	sb.WriteString("\n\n")

	for idx, entry := range entries {
		name := stringutil.TrimSpace(entry.ChannelName)
		if name == "" {
			name = "알 수 없는 채널"
		}

		sb.WriteString(groupAlarmEntryLine(idx+1, name, entry.ScheduledKST, minutesUntil))
		sb.WriteByte('\n')

		if entry.Title != "" {
			fmt.Fprintf(&sb, "   %s\n", entry.Title)
		}

		if entry.URL != "" {
			fmt.Fprintf(&sb, "   %s\n", entry.URL)
		}

		if idx < len(entries)-1 {
			sb.WriteString("\n")
		}
	}

	content := stringutil.TrimSpace(sb.String())
	if content == "" {
		return ""
	}

	return util.ApplyKakaoSeeMorePadding(content, instruction)
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
