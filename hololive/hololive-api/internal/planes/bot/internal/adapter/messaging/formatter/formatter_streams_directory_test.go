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
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatLiveStreamsAndUpcomingAndSchedule(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdLiveStreams:     "라이브 목록\n{{range .Streams}}{{.ChannelName}}|{{.Title}}|{{.URL}}|{{.ViewerCount}}\n{{end}}",
		domain.TemplateKeyCmdUpcomingStreams: "예정 목록\n{{range .Streams}}{{.ChannelName}}|{{.TimeInfo}}|{{.URL}}\n{{end}}",
		domain.TemplateKeyCmdChannelSchedule: "채널 일정\n{{range .Streams}}{{if .IsLive}}LIVE{{else}}{{.TimeInfo}}{{end}}|{{.Title}}|{{.URL}}\n{{end}}",
	})
	formatter := NewResponseFormatter("!", renderer, WithMessageStrings(setupFormatterTestStore(t)))

	orgHololive := "Hololive"
	future := time.Now().Add(2 * time.Hour)
	viewer := 1234
	longTitle := strings.Repeat("가", constants.StringLimits.StreamTitle+20)
	streams := []*domain.Stream{
		{
			ID:             "abc123",
			Title:          longTitle,
			ChannelName:    "사쿠라 미코",
			StartScheduled: &future,
			ViewerCount:    &viewer,
			Channel:        &domain.Channel{Name: "사쿠라 미코", Org: &orgHololive},
			Status:         domain.StreamStatusUpcoming,
		},
	}

	live := formatter.FormatLiveStreams(t.Context(), streams)
	assert.Contains(t, live, "라이브 목록")
	assert.Contains(t, live, "[Holo] 사쿠라 미코")
	assert.Contains(t, live, "https://youtube.com/watch?v=abc123")
	assert.NotContains(t, live, "\u200b")

	upcoming := formatter.UpcomingStreams(t.Context(), streams, 12)
	assert.Contains(t, upcoming, "예정 목록")
	assert.Contains(t, upcoming, "https://youtube.com/watch?v=abc123")
	assert.NotContains(t, upcoming, "\u200b")

	channel := &domain.Channel{Name: "사쿠라 미코"}
	schedule := formatter.ChannelSchedule(t.Context(), channel, streams, 7)
	assert.Contains(t, schedule, "채널 일정")
	assert.Contains(t, schedule, "https://youtube.com/watch?v=abc123")
	assert.NotContains(t, schedule, "\u200b")

	emptyLive := formatter.FormatLiveStreams(t.Context(), nil)
	assert.Equal(t, "라이브 목록", emptyLive)

	errorRenderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{})
	errorFormatter := NewResponseFormatter("!", errorRenderer)
	assert.Equal(t, messagestrings.FallbackSentinel, errorFormatter.FormatLiveStreams(t.Context(), streams))
	assert.Equal(t, messagestrings.FallbackSentinel, errorFormatter.UpcomingStreams(t.Context(), streams, 12))
	assert.Equal(t, messagestrings.FallbackSentinel, errorFormatter.ChannelSchedule(t.Context(), channel, streams, 7))
}

func TestStreamListFormattersCapRenderedViews(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdLiveStreams:     "live count={{.Count}}\n{{range .Streams}}L {{.Title}}\n{{end}}",
		domain.TemplateKeyCmdUpcomingStreams: "upcoming count={{.Count}}\n{{range .Streams}}U {{.Title}}\n{{end}}",
	})
	formatter := NewResponseFormatter("!", renderer)

	streams := make([]*domain.Stream, streamListDisplayLimit+5)
	for i := range streams {
		streams[i] = &domain.Stream{ID: "stream", Title: "title", ChannelName: "channel"}
	}

	live := formatter.FormatLiveStreams(t.Context(), streams)
	assert.Contains(t, live, "live count=105")
	assert.Equal(t, streamListDisplayLimit, strings.Count(live, "L title"))

	upcoming := formatter.UpcomingStreams(t.Context(), streams, 24)
	assert.Contains(t, upcoming, "upcoming count=105")
	assert.Equal(t, streamListDisplayLimit, strings.Count(upcoming, "U title"))
}

func TestStreamHelpers(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdMemberNotLive:    "{{.MemberName}}은(는) 현재 방송 중이 아닙니다.",
		domain.TemplateKeyCmdMemberNoUpcoming: "{{.MemberName}}은(는) {{.Hours}}시간 이내 예정된 방송이 없습니다.",
	})
	formatter := NewResponseFormatter("!", renderer, WithMessageStrings(setupFormatterTestStore(t)))

	t.Run("truncate title", func(t *testing.T) {
		t.Parallel()

		input := strings.Repeat("A", constants.StringLimits.StreamTitle+30)
		got := formatter.truncateTitle(input)
		assert.LessOrEqual(t, len([]rune(got)), constants.StringLimits.StreamTitle+3)
	})

	t.Run("streamTimeInfo branches", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "시간 미정", formatter.streamTimeInfo(t.Context(), nil))
		assert.Equal(t, "시간 미정", formatter.streamTimeInfo(t.Context(), &domain.Stream{}))

		futureDays := time.Now().Add(50 * time.Hour)
		assert.Contains(t, formatter.streamTimeInfo(t.Context(), &domain.Stream{StartScheduled: &futureDays}), "일 후")

		futureHours := time.Now().Add(3*time.Hour + 10*time.Minute)
		assert.Contains(t, formatter.streamTimeInfo(t.Context(), &domain.Stream{StartScheduled: &futureHours}), "시간")

		futureMinutes := time.Now().Add(20 * time.Minute)
		assert.Contains(t, formatter.streamTimeInfo(t.Context(), &domain.Stream{StartScheduled: &futureMinutes}), "분 후")

		past := time.Now().Add(-10 * time.Minute)
		assert.NotContains(t, formatter.streamTimeInfo(t.Context(), &domain.Stream{StartScheduled: &past}), "후")
	})

	t.Run("formatChannelName", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, formatter.formatChannelName(t.Context(), nil))

		org := "Nijisanji"
		stream := &domain.Stream{ChannelName: "쿠제 혼마", Channel: &domain.Channel{Org: &org}}
		assert.Equal(t, "[니지산지] 쿠제 혼마", formatter.formatChannelName(t.Context(), stream))

		unknownOrg := "NewOrg"

		stream = &domain.Stream{ChannelName: "테스트", Channel: &domain.Channel{Org: &unknownOrg}}
		assert.Equal(t, "[NewOrg] 테스트", formatter.formatChannelName(t.Context(), stream))

		stream = &domain.Stream{ChannelName: "채널명"}
		assert.Equal(t, "채널명", formatter.formatChannelName(t.Context(), stream))
	})

	assert.Equal(t, "미코은(는) 현재 방송 중이 아닙니다.", formatter.FormatMemberNotLive(t.Context(), "미코"))
	assert.Equal(t, "미코은(는) 12시간 이내 예정된 방송이 없습니다.", formatter.FormatMemberNoUpcoming(t.Context(), "미코", 12))
}

func TestPrepareMemberDirectoryGroupsAndMemberDirectory(t *testing.T) {
	t.Parallel()

	renderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{
		domain.TemplateKeyCmdMemberDirectory: "멤버 목록\n{{range .Groups}}{{.GroupName}}:{{range .Members}}{{.Primary}}{{if .ShowBoth}}({{.Secondary}}){{end}},{{end}}\n{{end}}",
	})
	formatter := NewResponseFormatter("!", renderer)

	groups := []MemberDirectoryGroup{
		{
			GroupName: "  JP 1기생  ",
			Members: []MemberDirectoryEntry{
				{PrimaryName: "사쿠라 미코", SecondaryName: "Sakura Miko"},
				{PrimaryName: "  ", SecondaryName: ""},
			},
		},
		{
			GroupName: "",
			Members: []MemberDirectoryEntry{
				{PrimaryName: "fubuki", SecondaryName: "FUBUKI"}, // ShowBoth false (equal fold)
			},
		},
	}

	prepared := prepareMemberDirectoryGroups(groups)
	require.Len(t, prepared, 2)
	assert.Equal(t, "JP 1기생", prepared[0].GroupName)
	assert.Equal(t, "기타", prepared[1].GroupName)
	require.Len(t, prepared[0].Members, 1)
	assert.True(t, prepared[0].Members[0].ShowBoth)
	require.Len(t, prepared[1].Members, 1)
	assert.False(t, prepared[1].Members[0].ShowBoth)

	message := formatter.MemberDirectory(t.Context(), groups, 0)
	assert.Contains(t, message, "멤버 목록")
	assert.Contains(t, message, "JP 1기생")
	assert.Contains(t, message, "사쿠라 미코(Sakura Miko)")
	assert.NotContains(t, message, "\u200b")

	emptyPrepared := prepareMemberDirectoryGroups(nil)
	assert.Nil(t, emptyPrepared)

	emptyMessage := formatter.MemberDirectory(t.Context(), nil, 0)
	assert.Equal(t, "멤버 목록", emptyMessage)

	errorRenderer := setupFormatterTestRenderer(t, map[domain.TemplateKey]string{})
	errorFormatter := NewResponseFormatter("!", errorRenderer)
	assert.Equal(t, messagestrings.FallbackSentinel, errorFormatter.MemberDirectory(t.Context(), groups, 1))
}

func TestFormatChannelName_IndependentsOrg(t *testing.T) {
	t.Parallel()

	f := &ResponseFormatter{messageStrings: setupFormatterTestStore(t)}

	tests := []struct {
		name   string
		stream *domain.Stream
		want   string
	}{
		{
			name: "Independents org shows 개인세 tag",
			stream: &domain.Stream{
				ChannelName: "유우키 사쿠나",
				Channel: &domain.Channel{
					Org: new("Independents"),
				},
			},
			want: "[개인세] 유우키 사쿠나",
		},
		{
			name: "Hololive org shows Holo tag",
			stream: &domain.Stream{
				ChannelName: "사쿠라 미코",
				Channel: &domain.Channel{
					Org: new("Hololive"),
				},
			},
			want: "[Holo] 사쿠라 미코",
		},
		{
			name:   "nil stream returns empty",
			stream: nil,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, f.formatChannelName(t.Context(), tt.stream))
		})
	}
}
