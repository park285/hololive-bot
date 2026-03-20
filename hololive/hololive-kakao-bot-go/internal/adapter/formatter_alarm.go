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
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// AlarmListEntry: 알림 목록 조회를 위한 개별 항목 (멤버 이름 및 다음 방송 정보 포함).
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
			case "Indie":
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

func (f *ResponseFormatter) FormatAlarmList(ctx context.Context, alarms []AlarmListEntry) string {
	processed := make([]alarmListEntryView, len(alarms))
	for idx, alarm := range alarms {
		processed[idx] = alarmListEntryView{
			MemberName: alarm.MemberName,
			TypesLabel: formatAlarmTypesLabel(alarm.AlarmTypes),
			NextStream: buildNextStreamInfoView(summarizeNextStreamInfo(alarm.NextStream)),
		}
	}

	data := alarmListTemplateData{
		Emoji:  DefaultEmoji,
		Count:  len(processed),
		Prefix: f.prefix,
		Alarms: processed,
	}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdAlarmList, data)
	if err != nil {
		return ErrorMessage(ErrDisplayAlarmListFailed)
	}

	if data.Count == 0 {
		return rendered
	}

	instruction, body := splitTemplateInstruction(rendered)
	if instruction == "" || body == "" {
		return rendered
	}

	return util.ApplyKakaoSeeMorePadding(body, instruction)
}

func (f *ResponseFormatter) FormatAlarmCleared(ctx context.Context, count int) string {
	data := alarmClearedTemplateData{Emoji: DefaultEmoji, Count: count}

	rendered, err := f.render(ctx, domain.TemplateKeyCmdAlarmCleared, data)
	if err != nil {
		return ErrorMessage(ErrDisplayAlarmClearFailed)
	}

	return rendered
}

// InvalidAlarmUsage: 알림 명령어의 잘못된 사용법에 대한 안내 메시지를 반환합니다.
func (f *ResponseFormatter) InvalidAlarmUsage() string {
	return ErrInvalidAlarmUsage
}

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

// AlarmNotificationGroup: 여러 방송의 알림을 하나로 묶어 그룹 메시지를 생성한다. (알림 폭탄 방지).
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

type milestoneAchievedTemplateData struct {
	MemberName string
	Milestone  string
}

type milestoneApproachingTemplateData struct {
	MemberName string
	Milestone  string
	Remaining  string
}

func (f *ResponseFormatter) FormatMilestoneAchieved(ctx context.Context, memberName, milestone string) (string, error) {
	data := milestoneAchievedTemplateData{
		MemberName: memberName,
		Milestone:  milestone,
	}

	return f.render(ctx, domain.TemplateKeyCmdMilestoneAchieved, data)
}

func (f *ResponseFormatter) FormatMilestoneApproaching(ctx context.Context, memberName, milestone, remaining string) (string, error) {
	data := milestoneApproachingTemplateData{
		MemberName: memberName,
		Milestone:  milestone,
		Remaining:  remaining,
	}

	return f.render(ctx, domain.TemplateKeyCmdMilestoneApproach, data)
}

func formatAlarmTypesLabel(types domain.AlarmTypes) string {
	if len(types) == 0 || len(types) == len(domain.AllAlarmTypes) {
		return "전체"
	}

	names := make([]string, len(types))
	for i, t := range types {
		names[i] = t.DisplayName()
	}

	return strings.Join(names, "+")
}

func (f *ResponseFormatter) FormatAmbiguousMembers(candidates []*domain.Member) string {
	var sb strings.Builder
	sb.WriteString("동일한 이름의 멤버가 여러 명 있습니다:\n\n")

	for i, m := range candidates {
		fmt.Fprintf(&sb, "%d. %s\n", i+1, m.GetDisplayName())
	}

	sb.WriteString("\n정확한 멤버를 지정하려면 다음과 같이 입력해주세요:\n")

	if len(candidates) > 0 {
		fmt.Fprintf(&sb, "%s알람 추가 %s", f.prefix, candidates[0].GetDisplayName())
	}

	return sb.String()
}
