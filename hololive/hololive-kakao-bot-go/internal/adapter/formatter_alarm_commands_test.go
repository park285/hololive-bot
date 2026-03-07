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
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

func TestAlarmFormatters_CommandPaths(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdAlarmAdded:        "ADD {{.MemberName}} {{.Added}} {{if .NextStream}}{{.NextStream.Status}}{{end}} {{.Prefix}}",
		domain.TemplateKeyCmdAlarmRemoved:      "REMOVE {{.MemberName}} {{.Removed}}",
		domain.TemplateKeyCmdAlarmList:         "알람 목록\n{{range .Alarms}}{{.MemberName}}|{{.TypesLabel}}{{if .NextStream}}|{{.NextStream.Status}}{{end}}\n{{end}}",
		domain.TemplateKeyCmdAlarmCleared:      "CLEAR {{.Count}}",
		domain.TemplateKeyCmdAlarmNotification: "NOTIFY {{.ChannelName}} {{.ScheduledTimeKST}} {{.URL}}",
		domain.TemplateKeyCmdAlarmLiveStarted:  "LIVE {{.ChannelName}} {{.ScheduledTimeKST}} {{.URL}}",
		domain.TemplateKeyCmdMilestoneAchieved: "MILESTONE {{.MemberName}} {{.Milestone}}",
		domain.TemplateKeyCmdMilestoneApproach: "APPROACH {{.MemberName}} {{.Milestone}} {{.Remaining}}",
	})
	formatter := NewResponseFormatter("!", renderer)

	now := time.Now().Add(2 * time.Hour)
	nextUpcoming := &domain.NextStreamInfo{
		Status:         domain.NextStreamStatusUpcoming,
		VideoID:        "abc123",
		Title:          "다음 방송",
		StartScheduled: &now,
	}
	added := formatter.FormatAlarmAdded(context.Background(), "미코", true, nextUpcoming)
	assert.Contains(t, added, "ADD 미코 true")
	assert.Contains(t, added, "upcoming")

	removed := formatter.FormatAlarmRemoved(context.Background(), "미코", true)
	assert.Equal(t, "REMOVE 미코 true", removed)

	list := formatter.FormatAlarmList(context.Background(), []AlarmListEntry{
		{MemberName: "미코", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive, domain.AlarmTypeCommunity}, NextStream: &domain.NextStreamInfo{Status: domain.NextStreamStatusLive, VideoID: "live123", Title: "라이브"}},
	})
	assert.Contains(t, list, "알람 목록")
	assert.Contains(t, list, "미코|방송+커뮤니티|live")
	assert.Equal(t, util.KakaoSeeMorePadding, strings.Count(list, util.KakaoZeroWidthSpace))

	emptyList := formatter.FormatAlarmList(context.Background(), nil)
	assert.Equal(t, "알람 목록", emptyList)

	assert.Equal(t, "CLEAR 3", formatter.FormatAlarmCleared(context.Background(), 3))
	assert.Equal(t, ErrInvalidAlarmUsage, formatter.InvalidAlarmUsage())

	notify := formatter.AlarmNotification(context.Background(), &domain.AlarmNotification{
		Channel: &domain.Channel{Name: "미코"},
		Stream: &domain.Stream{
			ID:             "yt123",
			Title:          "방송",
			ChannelName:    "미코",
			StartScheduled: &now,
		},
		MinutesUntil: 5,
	})
	assert.Contains(t, notify, "NOTIFY")
	assert.Contains(t, notify, "https://youtube.com/watch?v=yt123")

	liveStarted := formatter.AlarmNotification(context.Background(), &domain.AlarmNotification{
		Channel: &domain.Channel{Name: "후부키"},
		Stream: &domain.Stream{
			ID:             "yt999",
			Title:          "시작",
			ChannelName:    "후부키",
			StartScheduled: &now,
		},
		MinutesUntil: 0,
	})
	assert.Contains(t, liveStarted, "LIVE")

	milestoneMsg, err := formatter.FormatMilestoneAchieved(context.Background(), "미코", "100만")
	require.NoError(t, err)
	assert.Equal(t, "MILESTONE 미코 100만", milestoneMsg)

	approachMsg, err := formatter.FormatMilestoneApproaching(context.Background(), "미코", "100만", "5천")
	require.NoError(t, err)
	assert.Equal(t, "APPROACH 미코 100만 5천", approachMsg)
}

func TestAlarmFormatters_FallbackAndHelpers(t *testing.T) {
	t.Parallel()

	formatter := NewResponseFormatter("!", setupFormatterTestRenderer(t, map[domain.TemplateKey]string{}))
	assert.Equal(t, ErrorMessage(ErrDisplayAlarmAddFailed), formatter.FormatAlarmAdded(context.Background(), "미코", true, nil))
	assert.Equal(t, ErrorMessage(ErrDisplayAlarmRemoveFailed), formatter.FormatAlarmRemoved(context.Background(), "미코", true))
	assert.Equal(t, ErrorMessage(ErrDisplayAlarmListFailed), formatter.FormatAlarmList(context.Background(), []AlarmListEntry{{MemberName: "미코"}}))
	assert.Equal(t, ErrorMessage(ErrDisplayAlarmClearFailed), formatter.FormatAlarmCleared(context.Background(), 1))
	assert.Equal(t, ErrorMessage(ErrDisplayAlarmNotifyFailed), formatter.AlarmNotification(context.Background(), &domain.AlarmNotification{MinutesUntil: 1, Stream: &domain.Stream{ID: "yt", Title: "t", ChannelName: "c"}}))

	fallbackLive := formatter.AlarmNotification(context.Background(), &domain.AlarmNotification{MinutesUntil: 0, Channel: &domain.Channel{Name: "미코"}, Stream: &domain.Stream{ID: "yt", Title: "제목", ChannelName: "미코"}})
	assert.Contains(t, fallbackLive, "방송 시작됨")
	assert.Contains(t, fallbackLive, "https://youtube.com/watch?v=yt")

	assert.Nil(t, summarizeNextStreamInfo(nil))
	assert.Nil(t, summarizeNextStreamInfo(&domain.NextStreamInfo{Status: domain.NextStreamStatusUpcoming}))
	require.NotNil(t, summarizeNextStreamInfo(&domain.NextStreamInfo{Status: domain.NextStreamStatusLive}))

	assert.Nil(t, buildNextStreamInfoView(nil))
	assert.Nil(t, buildNextStreamInfoView(&domain.NextStreamInfo{Status: "invalid"}))

	future := time.Now().Add(90 * time.Minute)
	view := buildNextStreamInfoView(&domain.NextStreamInfo{Status: domain.NextStreamStatusUpcoming, VideoID: "v1", Title: strings.Repeat("A", 10), StartScheduled: &future})
	require.NotNil(t, view)
	assert.Equal(t, "upcoming", view.Status)
	assert.NotEmpty(t, view.ScheduledKST)
	assert.NotEmpty(t, view.TimeDetail)

	past := time.Now().Add(-2 * time.Minute)
	soon := buildNextStreamInfoView(&domain.NextStreamInfo{Status: domain.NextStreamStatusUpcoming, VideoID: "v2", Title: "soon", StartScheduled: &past})
	require.NotNil(t, soon)
	assert.True(t, soon.StartingSoon)

	assert.Equal(t, "", formatUpcomingTimeDetail(-time.Minute))
	assert.Equal(t, "30분 후", formatUpcomingTimeDetail(30*time.Minute))
	assert.Equal(t, "2시간 0분 후", formatUpcomingTimeDetail(2*time.Hour))
	assert.Equal(t, "1일 후", formatUpcomingTimeDetail(26*time.Hour))

	assert.Equal(t, "전체", formatAlarmTypesLabel(nil))
	assert.Equal(t, "전체", formatAlarmTypesLabel(domain.AlarmTypes(domain.AllAlarmTypes)))
	assert.Equal(t, "방송+쇼츠", formatAlarmTypesLabel(domain.AlarmTypes{domain.AlarmTypeLive, domain.AlarmTypeShorts}))

	ambiguous := formatter.FormatAmbiguousMembers([]*domain.Member{{Name: "미코", Org: "Hololive"}, {Name: "미코", Org: "Nijisanji"}})
	assert.Contains(t, ambiguous, "동일한 이름의 멤버가 여러 명")
	assert.Contains(t, ambiguous, "미코 (Hololive)")
	assert.Contains(t, ambiguous, "!알람 추가")
}
