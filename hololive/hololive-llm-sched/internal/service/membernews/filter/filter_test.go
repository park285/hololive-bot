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

package filter

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-llm-sched/internal/service/membernews/internal/model"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type testSourceValidator struct{}

func (v *testSourceValidator) ValidateSourceURL(rawURL string) (model.SourceTier, string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return model.SourceTierCommunity, "", fmt.Errorf("source url is empty")
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return model.SourceTierCommunity, "", fmt.Errorf("parse source url: %w", err)
	}

	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	host = strings.TrimPrefix(host, "www.")

	switch host {
	case "hololive.hololivepro.com", "hololivepro.com", "cover-corp.com":
		return model.SourceTierOfficial, parsed.String(), nil
	case "prtimes.jp", "oricon.co.jp", "natalie.mu", "famitsu.com", "4gamer.net", "animate.tv", "dengekionline.com":
		return model.SourceTierMedia, parsed.String(), nil
	default:
		return model.SourceTierCommunity, parsed.String(), nil
	}
}

func (v *testSourceValidator) HasCorroboration(text string) bool {
	for _, needle := range []string{
		"hololive.hololivepro.com",
		"hololivepro.com",
		"cover-corp.com",
		"prtimes.jp",
		"oricon.co.jp",
		"natalie.mu",
		"famitsu.com",
		"4gamer.net",
		"animate.tv",
		"dengekionline.com",
	} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

type mockMemberDataForFilter struct {
	byName  map[string]*domain.Member
	byAlias map[string]*domain.Member
}

func (m *mockMemberDataForFilter) FindMemberByChannelID(_ string) *domain.Member { return nil }
func (m *mockMemberDataForFilter) FindMemberByName(name string) *domain.Member {
	if m.byName == nil {
		return nil
	}
	return m.byName[name]
}
func (m *mockMemberDataForFilter) FindMemberByAlias(alias string) *domain.Member {
	if m.byAlias == nil {
		return nil
	}
	return m.byAlias[alias]
}
func (m *mockMemberDataForFilter) GetChannelIDs() []string         { return nil }
func (m *mockMemberDataForFilter) GetAllMembers() []*domain.Member { return nil }
func (m *mockMemberDataForFilter) WithContext(_ context.Context) domain.MemberDataProvider {
	return m
}
func (m *mockMemberDataForFilter) FindMembersByName(_ string) []*domain.Member  { return nil }
func (m *mockMemberDataForFilter) FindMembersByAlias(_ string) []*domain.Member { return nil }

func TestFilterCandidates_PeriodAndSorting(t *testing.T) {
	validator := &testSourceValidator{}

	now := time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST)
	targetDate := time.Date(2026, 2, 20, 12, 0, 0, 0, model.KST)
	farFuture := time.Date(2026, 6, 1, 12, 0, 0, 0, model.KST)

	candidates := []model.Candidate{
		{
			Type:           "event",
			Title:          "사쿠라 미코 공식 행사",
			Description:    "official",
			EventStartDate: &targetDate,
			SourceURL:      "https://hololive.hololivepro.com/events/1",
		},
		{
			Type:           "event",
			Title:          "사쿠라 미코 커뮤니티 행사",
			Description:    "official corroboration: https://hololive.hololivepro.com/news/1",
			EventStartDate: &targetDate,
			SourceURL:      "https://example.com/post/1",
		},
		{
			Type:           "event",
			Title:          "사쿠라 미코 먼 미래 행사",
			EventStartDate: &farFuture,
			SourceURL:      "https://hololive.hololivepro.com/events/2",
		},
	}

	filtered := FilterCandidates(candidates, model.PeriodWeekly, now, []string{"사쿠라 미코"}, nil, validator)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 candidates in weekly range, got %d", len(filtered))
	}

	if filtered[0].SourceTier != model.SourceTierOfficial {
		t.Fatalf("expected first candidate official tier, got %s", filtered[0].SourceTier)
	}
	if filtered[1].SourceTier != model.SourceTierCommunity {
		t.Fatalf("expected second candidate community tier, got %s", filtered[1].SourceTier)
	}
}

func TestClassifyCategory(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		description string
		want        model.Category
	}{
		{"生誕 in title", "生誕ライブ開催", "", model.CategoryBirthdayLive},
		{"생일 keyword", "사쿠라 미코 생일", "", model.CategoryBirthdayLive},
		{"birthday in description", "special event", "birthday celebration", model.CategoryBirthdayLive},
		{"birthday priority over live/event", "Birthday Live Concert event", "", model.CategoryBirthdayLive},

		{"ソロライブ", "ソロライブ開催", "", model.CategorySoloLive},
		{"solo live", "solo live announced", "", model.CategorySoloLive},
		{"단독 라이브", "단독 라이브 개최", "", model.CategorySoloLive},
		{"solo live priority over event keyword", "solo live concert event", "", model.CategorySoloLive},

		{"コラボ", "コラボイベント", "", model.CategoryCollab},
		{"콜라보", "콜라보 카페", "", model.CategoryCollab},
		{"collaboration in description", "event info", "collaboration details", model.CategoryCollab},

		{"グッズ", "新グッズ販売", "", model.CategoryGoods},
		{"굿즈", "굿즈 판매", "", model.CategoryGoods},
		{"merchandise", "new merchandise", "", model.CategoryGoods},

		{"fes keyword", "hololive fes 2026", "", model.CategoryEvent},
		{"expo keyword", "SUPER EXPO 2026", "", model.CategoryEvent},
		{"concert keyword", "holo concert", "", model.CategoryEvent},
		{"event keyword", "special event announcement", "", model.CategoryEvent},
		{"live keyword without qualifier", "big live show", "", model.CategoryEvent},

		{"no match → CategoryOther", "一般的なお知らせ", "追加情報なし", model.CategoryOther},

		{"title+desc combined: solo in title, live in desc", "special solo", "live show details", model.CategorySoloLive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyCategory(model.Candidate{Title: tt.title, Description: tt.description})
			if got != tt.want {
				t.Errorf("classifyCategory(title=%q, desc=%q) = %q, want %q",
					tt.title, tt.description, got, tt.want)
			}
		})
	}
}

func TestMatchMembers(t *testing.T) {
	tests := []struct {
		name      string
		candidate model.Candidate
		profiles  []memberProfile
		wantLen   int
		want      []string // nil이면 길이만 확인
	}{
		{
			name:      "candidate.Members token exact match",
			candidate: model.Candidate{Members: []string{"사쿠라 미코"}, Title: "unrelated", Description: "info"},
			profiles:  []memberProfile{{display: "사쿠라 미코", tokens: []string{"사쿠라미코"}}},
			wantLen:   1,
			want:      []string{"사쿠라 미코"},
		},
		{
			name:      "body match via title",
			candidate: model.Candidate{Title: "사쿠라 미코 solo live", Description: "details"},
			profiles:  []memberProfile{{display: "사쿠라 미코", tokens: []string{"사쿠라미코"}}},
			wantLen:   1,
			want:      []string{"사쿠라 미코"},
		},
		{
			name:      "body match via description",
			candidate: model.Candidate{Title: "event announcement", Description: "featuring 사쿠라 미코"},
			profiles:  []memberProfile{{display: "사쿠라 미코", tokens: []string{"사쿠라미코"}}},
			wantLen:   1,
			want:      []string{"사쿠라 미코"},
		},
		{
			name:      "alias token match via Members field",
			candidate: model.Candidate{Members: []string{"sakuramiko"}, Title: "event", Description: "info"},
			profiles:  []memberProfile{{display: "사쿠라 미코", tokens: []string{"사쿠라미코", "sakuramiko"}}},
			wantLen:   1,
			want:      []string{"사쿠라 미코"},
		},
		{
			name:      "empty profiles returns nil",
			candidate: model.Candidate{Members: []string{"사쿠라 미코"}, Title: "event"},
			profiles:  nil,
			wantLen:   0,
		},
		{
			name:      "dedup: same display name from duplicate profiles",
			candidate: model.Candidate{Members: []string{"사쿠라 미코"}, Title: "사쿠라 미코 event"},
			profiles: []memberProfile{
				{display: "사쿠라 미코", tokens: []string{"사쿠라미코"}},
				{display: "사쿠라 미코", tokens: []string{"사쿠라미코"}},
			},
			wantLen: 1,
			want:    []string{"사쿠라 미코"},
		},
		{
			name:      "no match returns empty",
			candidate: model.Candidate{Members: []string{"unknown"}, Title: "unrelated", Description: "desc"},
			profiles:  []memberProfile{{display: "사쿠라 미코", tokens: []string{"사쿠라미코"}}},
			wantLen:   0,
		},
		{
			name: "multiple members match preserves order",
			candidate: model.Candidate{
				Members: []string{"사쿠라 미코", "호시마치 스이세이"},
				Title:   "collab event",
			},
			profiles: []memberProfile{
				{display: "사쿠라 미코", tokens: []string{"사쿠라미코"}},
				{display: "호시마치 스이세이", tokens: []string{"호시마치스이세이"}},
			},
			wantLen: 2,
			want:    []string{"사쿠라 미코", "호시마치 스이세이"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchMembers(tt.candidate, tt.profiles)
			if len(got) != tt.wantLen {
				t.Fatalf("len = %d %v, want %d", len(got), got, tt.wantLen)
			}
			for i, w := range tt.want {
				if i >= len(got) {
					break
				}
				if got[i] != w {
					t.Errorf("[%d] = %q, want %q", i, got[i], w)
				}
			}
		})
	}
}

func TestApplyPeriodFilter(t *testing.T) {
	now := time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST)

	t.Run("weekly: in-range passes, out-of-range excluded", func(t *testing.T) {
		inRange := time.Date(2026, 2, 20, 12, 0, 0, 0, model.KST)
		outOfRange := time.Date(2026, 6, 1, 12, 0, 0, 0, model.KST)
		candidates := []model.Candidate{
			{EventStartDate: &inRange, Type: domain.MajorEventTypeEvent},
			{EventStartDate: &outOfRange, Type: domain.MajorEventTypeEvent},
		}
		result := applyPeriodFilter(candidates, model.PeriodWeekly, now)
		if len(result) != 1 {
			t.Fatalf("expected 1, got %d", len(result))
		}
	})

	t.Run("monthly: month boundary check", func(t *testing.T) {
		inRange := time.Date(2026, 2, 15, 12, 0, 0, 0, model.KST)
		outOfRange := time.Date(2026, 3, 5, 12, 0, 0, 0, model.KST)
		candidates := []model.Candidate{
			{EventStartDate: &inRange, Type: domain.MajorEventTypeEvent},
			{EventStartDate: &outOfRange, Type: domain.MajorEventTypeEvent},
		}
		result := applyPeriodFilter(candidates, model.PeriodMonthly, now)
		if len(result) != 1 {
			t.Fatalf("expected 1, got %d", len(result))
		}
	})

	t.Run("news type: PubDate takes priority over EventStartDate", func(t *testing.T) {
		pubDate := time.Date(2026, 2, 15, 12, 0, 0, 0, model.KST)
		eventDate := time.Date(2026, 6, 1, 12, 0, 0, 0, model.KST) // 범위 밖
		candidates := []model.Candidate{
			{Type: domain.MajorEventTypeNews, PubDate: &pubDate, EventStartDate: &eventDate},
		}
		result := applyPeriodFilter(candidates, model.PeriodWeekly, now)
		if len(result) != 1 {
			t.Fatalf("expected 1 (news uses PubDate in range), got %d", len(result))
		}
		if !result[0].date.Equal(pubDate.In(model.KST)) {
			t.Fatalf("expected effective date = PubDate %v, got %v", pubDate.In(model.KST), result[0].date)
		}
	})

	t.Run("event type: EventStartDate takes priority over PubDate", func(t *testing.T) {
		pubDate := time.Date(2026, 6, 1, 12, 0, 0, 0, model.KST)    // 범위 밖
		eventDate := time.Date(2026, 2, 20, 12, 0, 0, 0, model.KST) // 범위 내
		candidates := []model.Candidate{
			{Type: domain.MajorEventTypeEvent, PubDate: &pubDate, EventStartDate: &eventDate},
		}
		result := applyPeriodFilter(candidates, model.PeriodWeekly, now)
		if len(result) != 1 {
			t.Fatalf("expected 1 (event uses EventStartDate in range), got %d", len(result))
		}
		if !result[0].date.Equal(eventDate.In(model.KST)) {
			t.Fatalf("expected effective date = EventStartDate %v, got %v", eventDate.In(model.KST), result[0].date)
		}
	})

	t.Run("both dates nil → excluded", func(t *testing.T) {
		candidates := []model.Candidate{
			{Type: domain.MajorEventTypeEvent},
		}
		result := applyPeriodFilter(candidates, model.PeriodWeekly, now)
		if len(result) != 0 {
			t.Fatalf("expected 0 (both dates nil), got %d", len(result))
		}
	})
}

func TestBuildMemberProfiles(t *testing.T) {
	t.Run("nil membersData → display name token only", func(t *testing.T) {
		profiles := buildMemberProfiles([]string{"사쿠라 미코"}, nil)
		if len(profiles) != 1 {
			t.Fatalf("expected 1 profile, got %d", len(profiles))
		}
		if profiles[0].display != "사쿠라 미코" {
			t.Fatalf("expected display %q, got %q", "사쿠라 미코", profiles[0].display)
		}
		if len(profiles[0].tokens) != 1 {
			t.Fatalf("expected 1 token, got %d: %v", len(profiles[0].tokens), profiles[0].tokens)
		}
	})

	t.Run("membersData hit → NameKo/NameJa/Aliases included", func(t *testing.T) {
		mock := &mockMemberDataForFilter{
			byName: map[string]*domain.Member{
				"사쿠라 미코": {
					Name:   "Sakura Miko",
					NameKo: "사쿠라 미코",
					NameJa: "さくらみこ",
					Aliases: &domain.Aliases{
						Ko: []string{"미코"},
						Ja: []string{"みこち"},
					},
				},
			},
		}
		profiles := buildMemberProfiles([]string{"사쿠라 미코"}, mock)
		if len(profiles) != 1 {
			t.Fatalf("expected 1 profile, got %d", len(profiles))
		}
		if profiles[0].display != "사쿠라 미코" {
			t.Fatalf("expected display %q, got %q", "사쿠라 미코", profiles[0].display)
		}
		if len(profiles[0].tokens) <= 1 {
			t.Fatalf("expected more than 1 token (NameKo/NameJa/Aliases), got %d: %v",
				len(profiles[0].tokens), profiles[0].tokens)
		}
		if !slices.Contains(profiles[0].tokens, "sakuramiko") {
			t.Fatalf("expected token 'sakuramiko' from Name field, tokens: %v", profiles[0].tokens)
		}
	})

	t.Run("FindMemberByName miss → FindMemberByAlias fallback", func(t *testing.T) {
		mock := &mockMemberDataForFilter{
			byAlias: map[string]*domain.Member{
				"미코치": {
					Name:   "Sakura Miko",
					NameKo: "사쿠라 미코",
					NameJa: "さくらみこ",
				},
			},
		}
		profiles := buildMemberProfiles([]string{"미코치"}, mock)
		if len(profiles) != 1 {
			t.Fatalf("expected 1 profile, got %d", len(profiles))
		}
		if profiles[0].display != "사쿠라 미코" {
			t.Fatalf("expected display %q (from NameKo), got %q", "사쿠라 미코", profiles[0].display)
		}
		if len(profiles[0].tokens) <= 1 {
			t.Fatalf("expected additional tokens from alias-matched member, got %d: %v",
				len(profiles[0].tokens), profiles[0].tokens)
		}
	})

	t.Run("empty roomMembers → empty result", func(t *testing.T) {
		profiles := buildMemberProfiles(nil, nil)
		if len(profiles) != 0 {
			t.Fatalf("expected 0, got %d", len(profiles))
		}
	})
}

func TestFilterCandidates_EmptySourceURL(t *testing.T) {
	validator := &testSourceValidator{}

	now := time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST)
	date := time.Date(2026, 2, 20, 12, 0, 0, 0, model.KST)

	candidates := []model.Candidate{
		{Title: "사쿠라 미코 event", EventStartDate: &date, Type: domain.MajorEventTypeEvent, SourceURL: ""},
		{Title: "사쿠라 미코 event2", EventStartDate: &date, Type: domain.MajorEventTypeEvent, SourceURL: "  "},
	}

	filtered := FilterCandidates(candidates, model.PeriodWeekly, now, []string{"사쿠라 미코"}, nil, validator)
	if len(filtered) != 0 {
		t.Fatalf("expected 0 (empty sourceURL excluded), got %d", len(filtered))
	}
}

func TestFilterCandidates_CommunityWithoutCorroboration(t *testing.T) {
	validator := &testSourceValidator{}

	now := time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST)
	date := time.Date(2026, 2, 20, 12, 0, 0, 0, model.KST)

	candidates := []model.Candidate{
		{
			Title:          "사쿠라 미코 event",
			Description:    "비공식 정보만 포함됨",
			EventStartDate: &date,
			Type:           domain.MajorEventTypeEvent,
			SourceURL:      "https://example.com/post/1",
		},
	}

	filtered := FilterCandidates(candidates, model.PeriodWeekly, now, []string{"사쿠라 미코"}, nil, validator)
	if len(filtered) != 0 {
		t.Fatalf("expected 0 (community without corroboration excluded), got %d", len(filtered))
	}
}

func TestFilterCandidates_SortStability(t *testing.T) {
	validator := &testSourceValidator{}

	now := time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST)
	date1 := time.Date(2026, 2, 18, 12, 0, 0, 0, model.KST)
	date2 := time.Date(2026, 2, 20, 12, 0, 0, 0, model.KST)

	candidates := []model.Candidate{
		{
			Title: "Z-title 사쿠라 미코 event", Description: "official",
			EventStartDate: &date2, Type: domain.MajorEventTypeEvent,
			SourceURL: "https://hololive.hololivepro.com/events/3",
		},
		{
			Title: "A-title 사쿠라 미코 event", Description: "official",
			EventStartDate: &date2, Type: domain.MajorEventTypeEvent,
			SourceURL: "https://hololive.hololivepro.com/events/2",
		},
		{
			Title: "M-title 사쿠라 미코 event", Description: "official",
			EventStartDate: &date1, Type: domain.MajorEventTypeEvent,
			SourceURL: "https://hololive.hololivepro.com/events/1",
		},
	}

	filtered := FilterCandidates(candidates, model.PeriodWeekly, now, []string{"사쿠라 미코"}, nil, validator)
	if len(filtered) != 3 {
		t.Fatalf("expected 3, got %d", len(filtered))
	}

	if filtered[0].Candidate.Title != "M-title 사쿠라 미코 event" {
		t.Errorf("[0] expected earliest date (M-title), got %q", filtered[0].Candidate.Title)
	}
	if filtered[1].Candidate.Title != "A-title 사쿠라 미코 event" {
		t.Errorf("[1] expected alphabetically first (A-title), got %q", filtered[1].Candidate.Title)
	}
	if filtered[2].Candidate.Title != "Z-title 사쿠라 미코 event" {
		t.Errorf("[2] expected alphabetically last (Z-title), got %q", filtered[2].Candidate.Title)
	}
}

func TestFilterCandidates_MultipleMatchedMembers(t *testing.T) {
	validator := &testSourceValidator{}

	now := time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST)
	date := time.Date(2026, 2, 20, 12, 0, 0, 0, model.KST)

	candidates := []model.Candidate{
		{
			Title:          "사쿠라 미코 호시마치 스이세이 콜라보",
			Description:    "official collab",
			Members:        []string{"사쿠라 미코", "호시마치 스이세이"},
			EventStartDate: &date,
			Type:           domain.MajorEventTypeEvent,
			SourceURL:      "https://hololive.hololivepro.com/events/collab1",
		},
	}

	filtered := FilterCandidates(candidates, model.PeriodWeekly, now,
		[]string{"사쿠라 미코", "호시마치 스이세이"}, nil, validator)
	if len(filtered) != 1 {
		t.Fatalf("expected 1, got %d", len(filtered))
	}
	if len(filtered[0].MatchedMembers) != 2 {
		t.Fatalf("expected 2 matched members, got %d: %v",
			len(filtered[0].MatchedMembers), filtered[0].MatchedMembers)
	}
	if !strings.Contains(filtered[0].MemberText, ", ") {
		t.Fatalf("expected joined member text with comma, got %q", filtered[0].MemberText)
	}
}
