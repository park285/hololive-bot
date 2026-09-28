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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestAlarmFormatters_CommandPaths(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdAlarmAdded:        "ADD {{.MemberName}} {{.Added}} {{.Prefix}}",
		domain.TemplateKeyCmdAlarmRemoved:      "REMOVE {{.MemberName}} {{.Removed}}",
		domain.TemplateKeyCmdAlarmList:         "알람 목록\n{{range .Alarms}}{{.MemberName}}|{{.TypesLabel}}\n{{end}}",
		domain.TemplateKeyCmdAlarmCleared:      "CLEAR {{.Count}}",
		domain.TemplateKeyCmdAlarmNotification: "NOTIFY {{.ChannelName}} {{.ScheduledTimeKST}} {{.URL}}",
		domain.TemplateKeyCmdAlarmLiveStarted:  "LIVE {{.ChannelName}} {{.ScheduledTimeKST}} {{.URL}}",
		domain.TemplateKeyCmdAmbiguousMember:   "동일한 이름의 멤버가 여러 명 있습니다:\n\n{{range .Candidates}}{{.Index}}. {{.Name}}\n{{end}}\n정확한 멤버를 지정하려면 다음과 같이 입력해주세요:\n{{.Prefix}}{{.CommandExample}} {{.FirstName}}",
	})
	formatter := NewResponseFormatter("!", renderer, WithMessageStrings(setupFormatterTestStore(t)))

	now := time.Now().Add(2 * time.Hour)
	added := formatter.FormatAlarmAdded(t.Context(), "미코", true)
	assert.Equal(t, "ADD 미코 true !", added)

	removed := formatter.FormatAlarmRemoved(t.Context(), "미코", true)
	assert.Equal(t, "REMOVE 미코 true", removed)

	list := formatter.FormatAlarmList(t.Context(), []AlarmListEntry{
		{MemberName: "미코", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive, domain.AlarmTypeCommunity}},
	})
	assert.Equal(t, "알람 목록\n미코|방송+커뮤니티", list)

	emptyList := formatter.FormatAlarmList(t.Context(), nil)
	assert.Equal(t, "알람 목록", emptyList)

	assert.Equal(t, "CLEAR 3", formatter.FormatAlarmCleared(t.Context(), 3))

	notify := formatter.AlarmNotification(t.Context(), &domain.AlarmNotification{
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

	liveStarted := formatter.AlarmNotification(t.Context(), &domain.AlarmNotification{
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

	ambiguous := formatter.FormatAmbiguousMembers(t.Context(), []*domain.Member{{Name: "미코", Org: "Hololive"}, {Name: "미코", Org: "Nijisanji"}}, "라이브")
	assert.Contains(t, ambiguous, "동일한 이름의 멤버가 여러 명")
	assert.Contains(t, ambiguous, "1. 미코 (Hololive)")
	assert.Contains(t, ambiguous, "2. 미코 (Nijisanji)")
	assert.Contains(t, ambiguous, "!라이브 미코 (Hololive)")
}

func TestAlarmFormatters_FallbackAndHelpers(t *testing.T) {
	t.Parallel()

	formatter := NewResponseFormatter("!", setupFormatterTestRenderer(t, map[domain.TemplateKey]string{}), WithMessageStrings(setupFormatterTestStore(t)))
	assert.Equal(t, renderFailureMessage, formatter.FormatAlarmAdded(t.Context(), "미코", true))
	assert.Equal(t, renderFailureMessage, formatter.FormatAlarmRemoved(t.Context(), "미코", true))
	assert.Equal(t, renderFailureMessage, formatter.FormatAlarmList(t.Context(), []AlarmListEntry{{MemberName: "미코"}}))
	assert.Equal(t, renderFailureMessage, formatter.FormatAlarmCleared(t.Context(), 1))
	assert.Equal(t, renderFailureMessage, formatter.AlarmNotification(t.Context(), &domain.AlarmNotification{MinutesUntil: 1, Stream: &domain.Stream{ID: "yt", Title: "t", ChannelName: "c"}}))

	fallbackLive := formatter.AlarmNotification(t.Context(), &domain.AlarmNotification{MinutesUntil: 0, Channel: &domain.Channel{Name: "미코"}, Stream: &domain.Stream{ID: "yt", Title: "제목", ChannelName: "미코"}})
	assert.Equal(t, renderFailureMessage, fallbackLive)

	assert.Equal(t, "전체", formatter.formatAlarmTypesLabel(t.Context(), nil))
	assert.Equal(t, "전체", formatter.formatAlarmTypesLabel(t.Context(), domain.AlarmTypes(domain.AllAlarmTypes)))
	assert.Equal(t, "방송+쇼츠", formatter.formatAlarmTypesLabel(t.Context(), domain.AlarmTypes{domain.AlarmTypeLive, domain.AlarmTypeShorts}))

	assert.Equal(t, renderFailureMessage, formatter.FormatAmbiguousMembers(t.Context(), []*domain.Member{{Name: "미코", Org: "Hololive"}, {Name: "미코", Org: "Nijisanji"}}, "알람 추가"))
}
