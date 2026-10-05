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
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/timeutil"
)

const (
	testMemberMiko      = "사쿠라 미코"
	testMemberMikoAlias = "사쿠라미코"
	testMemberSuisei    = "호시마치 스이세이"
	testCandidateEvent  = "event"
)

type testSourceValidator struct{}

func (v *testSourceValidator) ValidateSourceURL(rawURL string) (model.SourceTier, string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return model.SourceTierCommunity, "", errors.New("source url is empty")
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
	byName    map[string]*domain.Member
	byAlias   map[string]*domain.Member
	nameErr   error
	aliasCall int
}

func (m *mockMemberDataForFilter) FindMemberByChannelID(context.Context, string) (*domain.Member, error) {
	return nil, domain.ErrMemberNotFound
}

func (m *mockMemberDataForFilter) FindMemberByName(_ context.Context, name string) (*domain.Member, error) {
	if m.nameErr != nil {
		return nil, m.nameErr
	}

	if member := m.byName[name]; member != nil {
		return member, nil
	}

	return nil, domain.ErrMemberNotFound
}

func (m *mockMemberDataForFilter) FindMemberByAlias(_ context.Context, alias string) (*domain.Member, error) {
	m.aliasCall++

	if member := m.byAlias[alias]; member != nil {
		return member, nil
	}

	return nil, domain.ErrMemberNotFound
}

func (m *mockMemberDataForFilter) GetChannelIDs(context.Context) ([]string, error) {
	return []string{}, nil
}

func (m *mockMemberDataForFilter) LoadAllMembers(context.Context) ([]*domain.Member, error) {
	return []*domain.Member{}, nil
}

func (m *mockMemberDataForFilter) FindMembersByName(context.Context, string) ([]*domain.Member, error) {
	return []*domain.Member{}, nil
}

func (m *mockMemberDataForFilter) FindMembersByAlias(context.Context, string) ([]*domain.Member, error) {
	return []*domain.Member{}, nil
}

// mustFilterCandidates는 멤버 데이터 없이 주간 기간으로 후보를 고른다.
func mustFilterCandidates(t *testing.T, candidates []model.Candidate, now time.Time, roomMembers []string, validator model.SourceURLValidator) []model.FilteredCandidate {
	t.Helper()

	filtered, err := FilterCandidates(t.Context(), candidates, model.PeriodWeekly, now, roomMembers, nil, validator)
	if err != nil {
		t.Fatalf("FilterCandidates() error = %v", err)
	}

	return filtered
}

func mustBuildMemberProfiles(t *testing.T, roomMembers []string, membersData domain.MemberDataProvider) []memberProfile {
	t.Helper()

	profiles, err := buildMemberProfiles(t.Context(), roomMembers, membersData)
	if err != nil {
		t.Fatalf("buildMemberProfiles() error = %v", err)
	}

	return profiles
}

// 이름 조회 실패는 별칭으로 넘기거나 원래 표기로 진행하지 않고, 후보 선별 전체를 오류로 끝낸다.
func TestFilterCandidates_MemberLookupFailureIsError(t *testing.T) {
	cause := errors.New("member cache unavailable")
	mock := &mockMemberDataForFilter{nameErr: cause}

	filtered, err := FilterCandidates(t.Context(), nil, model.PeriodWeekly, time.Now(), []string{testMemberMiko}, mock, &testSourceValidator{})
	if !errors.Is(err, cause) || filtered != nil {
		t.Fatalf("FilterCandidates() = %v, %v; want member lookup failure", filtered, err)
	}

	if mock.aliasCall != 0 {
		t.Fatalf("alias lookups = %d, want none after a name lookup failure", mock.aliasCall)
	}

	prepared, err := PrepareCandidates(nil, model.PeriodWeekly, time.Now()).Filter(t.Context(), []string{testMemberMiko}, mock, nil)
	if !errors.Is(err, cause) || prepared != nil {
		t.Fatalf("PreparedCandidates.Filter() = %v, %v; want member lookup failure", prepared, err)
	}
}

func TestFilterCandidates_PeriodAndSorting(t *testing.T) {
	validator := &testSourceValidator{}

	now := time.Date(2026, time.February, 16, 10, 0, 0, 0, timeutil.KSTZone)
	targetDate := time.Date(2026, time.February, 20, 12, 0, 0, 0, timeutil.KSTZone)
	farFuture := time.Date(2026, time.June, 1, 12, 0, 0, 0, timeutil.KSTZone)

	candidates := []model.Candidate{
		{
			Type:           testCandidateEvent,
			Title:          "사쿠라 미코 공식 행사",
			Description:    "official",
			EventStartDate: &targetDate,
			SourceURL:      "https://hololive.hololivepro.com/events/1",
		},
		{
			Type:           testCandidateEvent,
			Title:          "사쿠라 미코 커뮤니티 행사",
			Description:    "official corroboration: https://hololive.hololivepro.com/news/1",
			EventStartDate: &targetDate,
			SourceURL:      "https://example.com/post/1",
		},
		{
			Type:           testCandidateEvent,
			Title:          "사쿠라 미코 먼 미래 행사",
			EventStartDate: &farFuture,
			SourceURL:      "https://hololive.hololivepro.com/events/2",
		},
	}

	filtered := mustFilterCandidates(t, candidates, now, []string{testMemberMiko}, validator)
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
			got := classifyCategory(&model.Candidate{Title: tt.title, Description: tt.description})
			if got != tt.want {
				t.Errorf("classifyCategory(title=%q, desc=%q) = %q, want %q",
					tt.title, tt.description, got, tt.want)
			}
		})
	}
}

type matchMembersCase struct {
	name      string
	candidate model.Candidate
	profiles  []memberProfile
	wantLen   int
	want      []string // nil이면 길이만 확인
}

func matchMembersCases() []matchMembersCase {
	return []matchMembersCase{
		{
			name:      "candidate.Members token exact match",
			candidate: model.Candidate{Members: []string{testMemberMiko}, Title: "unrelated", Description: "info"},
			profiles:  []memberProfile{{display: testMemberMiko, tokens: []string{testMemberMikoAlias}}},
			wantLen:   1,
			want:      []string{testMemberMiko},
		},
		{
			name:      "body match via title",
			candidate: model.Candidate{Title: "사쿠라 미코 solo live", Description: "details"},
			profiles:  []memberProfile{{display: testMemberMiko, tokens: []string{testMemberMikoAlias}}},
			wantLen:   1,
			want:      []string{testMemberMiko},
		},
		{
			name:      "body match via description",
			candidate: model.Candidate{Title: "event announcement", Description: "featuring 사쿠라 미코"},
			profiles:  []memberProfile{{display: testMemberMiko, tokens: []string{testMemberMikoAlias}}},
			wantLen:   1,
			want:      []string{testMemberMiko},
		},
		{
			name:      "alias token match via Members field",
			candidate: model.Candidate{Members: []string{"sakuramiko"}, Title: testCandidateEvent, Description: "info"},
			profiles:  []memberProfile{{display: testMemberMiko, tokens: []string{testMemberMikoAlias, "sakuramiko"}}},
			wantLen:   1,
			want:      []string{testMemberMiko},
		},
		{
			name:      "empty profiles returns nil",
			candidate: model.Candidate{Members: []string{testMemberMiko}, Title: testCandidateEvent},
			profiles:  nil,
			wantLen:   0,
		},
		{
			name:      "dedup: same display name from duplicate profiles",
			candidate: model.Candidate{Members: []string{testMemberMiko}, Title: "사쿠라 미코 event"},
			profiles: []memberProfile{
				{display: testMemberMiko, tokens: []string{testMemberMikoAlias}},
				{display: testMemberMiko, tokens: []string{testMemberMikoAlias}},
			},
			wantLen: 1,
			want:    []string{testMemberMiko},
		},
		{
			name:      "no match returns empty",
			candidate: model.Candidate{Members: []string{"unknown"}, Title: "unrelated", Description: "desc"},
			profiles:  []memberProfile{{display: testMemberMiko, tokens: []string{testMemberMikoAlias}}},
			wantLen:   0,
		},
		{
			name: "multiple members match preserves order",
			candidate: model.Candidate{
				Members: []string{testMemberMiko, testMemberSuisei},
				Title:   "collab event",
			},
			profiles: []memberProfile{
				{display: testMemberMiko, tokens: []string{testMemberMikoAlias}},
				{display: testMemberSuisei, tokens: []string{"호시마치스이세이"}},
			},
			wantLen: 2,
			want:    []string{testMemberMiko, testMemberSuisei},
		},
	}
}

func TestMatchMembers(t *testing.T) {
	for _, tt := range matchMembersCases() {
		t.Run(tt.name, func(t *testing.T) {
			got := matchMembers(&tt.candidate, tt.profiles)
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

func periodFilterNow() time.Time {
	return time.Date(2026, time.February, 16, 10, 0, 0, 0, timeutil.KSTZone)
}

func TestApplyPeriodFilter(t *testing.T) {
	t.Run("weekly: in-range passes, out-of-range excluded", testApplyPeriodFilterWeekly)
	t.Run("monthly: month boundary check", testApplyPeriodFilterMonthly)
	t.Run("news type: PubDate takes priority over EventStartDate", testApplyPeriodFilterNewsPubDate)
	t.Run("event type: EventStartDate takes priority over PubDate", testApplyPeriodFilterEventStartDate)
	t.Run("both dates nil → excluded", testApplyPeriodFilterBothDatesNil)
}

func testApplyPeriodFilterWeekly(t *testing.T) {
	now := periodFilterNow()

	inRange := time.Date(2026, time.February, 20, 12, 0, 0, 0, timeutil.KSTZone)
	outOfRange := time.Date(2026, time.June, 1, 12, 0, 0, 0, timeutil.KSTZone)
	candidates := []model.Candidate{
		{EventStartDate: &inRange, Type: domain.MajorEventTypeEvent},
		{EventStartDate: &outOfRange, Type: domain.MajorEventTypeEvent},
	}
	result := applyPeriodFilter(candidates, model.PeriodWeekly, now)

	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
}

func testApplyPeriodFilterMonthly(t *testing.T) {
	now := periodFilterNow()

	inRange := time.Date(2026, time.February, 15, 12, 0, 0, 0, timeutil.KSTZone)
	outOfRange := time.Date(2026, time.March, 5, 12, 0, 0, 0, timeutil.KSTZone)
	candidates := []model.Candidate{
		{EventStartDate: &inRange, Type: domain.MajorEventTypeEvent},
		{EventStartDate: &outOfRange, Type: domain.MajorEventTypeEvent},
	}
	result := applyPeriodFilter(candidates, model.PeriodMonthly, now)

	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
}

func testApplyPeriodFilterNewsPubDate(t *testing.T) {
	now := periodFilterNow()

	pubDate := time.Date(2026, time.February, 15, 12, 0, 0, 0, timeutil.KSTZone)
	eventDate := time.Date(2026, time.June, 1, 12, 0, 0, 0, timeutil.KSTZone) // 범위 밖
	candidates := []model.Candidate{
		{Type: domain.MajorEventTypeNews, PubDate: &pubDate, EventStartDate: &eventDate},
	}
	result := applyPeriodFilter(candidates, model.PeriodWeekly, now)

	if len(result) != 1 {
		t.Fatalf("expected 1 (news uses PubDate in range), got %d", len(result))
	}

	if !result[0].date.Equal(pubDate.In(timeutil.KSTZone)) {
		t.Fatalf("expected effective date = PubDate %v, got %v", pubDate.In(timeutil.KSTZone), result[0].date)
	}
}

func testApplyPeriodFilterEventStartDate(t *testing.T) {
	now := periodFilterNow()

	pubDate := time.Date(2026, time.June, 1, 12, 0, 0, 0, timeutil.KSTZone)        // 범위 밖
	eventDate := time.Date(2026, time.February, 20, 12, 0, 0, 0, timeutil.KSTZone) // 범위 내
	candidates := []model.Candidate{
		{Type: domain.MajorEventTypeEvent, PubDate: &pubDate, EventStartDate: &eventDate},
	}
	result := applyPeriodFilter(candidates, model.PeriodWeekly, now)

	if len(result) != 1 {
		t.Fatalf("expected 1 (event uses EventStartDate in range), got %d", len(result))
	}

	if !result[0].date.Equal(eventDate.In(timeutil.KSTZone)) {
		t.Fatalf("expected effective date = EventStartDate %v, got %v", eventDate.In(timeutil.KSTZone), result[0].date)
	}
}

func testApplyPeriodFilterBothDatesNil(t *testing.T) {
	now := periodFilterNow()

	candidates := []model.Candidate{
		{Type: domain.MajorEventTypeEvent},
	}
	result := applyPeriodFilter(candidates, model.PeriodWeekly, now)

	if len(result) != 0 {
		t.Fatalf("expected 0 (both dates nil), got %d", len(result))
	}
}

func TestBuildMemberProfiles(t *testing.T) {
	t.Run("nil membersData → display name token only", testBuildMemberProfilesDisplayOnly)
	t.Run("membersData hit → NameKo/NameJa/Aliases included", testBuildMemberProfilesDataHit)
	t.Run("FindMemberByName miss → FindMemberByAlias fallback", testBuildMemberProfilesAliasFallback)

	t.Run("empty roomMembers → empty result", func(t *testing.T) {
		profiles := mustBuildMemberProfiles(t, nil, nil)
		if len(profiles) != 0 {
			t.Fatalf("expected 0, got %d", len(profiles))
		}
	})
}

func testBuildMemberProfilesDisplayOnly(t *testing.T) {
	profiles := mustBuildMemberProfiles(t, []string{testMemberMiko}, nil)
	requireSingleProfile(t, profiles, testMemberMiko)

	if len(profiles[0].tokens) != 1 {
		t.Fatalf("expected 1 token, got %d: %v", len(profiles[0].tokens), profiles[0].tokens)
	}
}

func testBuildMemberProfilesDataHit(t *testing.T) {
	mock := &mockMemberDataForFilter{
		byName: map[string]*domain.Member{
			testMemberMiko: {
				Name:   "Sakura Miko",
				NameKo: testMemberMiko,
				NameJa: "さくらみこ",
				Aliases: &domain.Aliases{
					Ko: []string{"미코"},
					Ja: []string{"みこち"},
				},
			},
		},
	}
	profiles := mustBuildMemberProfiles(t, []string{testMemberMiko}, mock)
	requireSingleProfile(t, profiles, testMemberMiko)
	requireAdditionalTokens(t, profiles)

	if !slices.Contains(profiles[0].tokens, "sakuramiko") {
		t.Fatalf("expected token 'sakuramiko' from Name field, tokens: %v", profiles[0].tokens)
	}
}

func testBuildMemberProfilesAliasFallback(t *testing.T) {
	mock := &mockMemberDataForFilter{
		byAlias: map[string]*domain.Member{
			"미코치": {
				Name:   "Sakura Miko",
				NameKo: testMemberMiko,
				NameJa: "さくらみこ",
			},
		},
	}
	profiles := mustBuildMemberProfiles(t, []string{"미코치"}, mock)
	requireSingleProfile(t, profiles, testMemberMiko)
	requireAdditionalTokens(t, profiles)
}

func requireSingleProfile(t *testing.T, profiles []memberProfile, wantDisplay string) {
	t.Helper()

	if len(profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(profiles))
	}

	if profiles[0].display != wantDisplay {
		t.Fatalf("expected display %q, got %q", wantDisplay, profiles[0].display)
	}
}

func requireAdditionalTokens(t *testing.T, profiles []memberProfile) {
	t.Helper()

	if len(profiles[0].tokens) <= 1 {
		t.Fatalf("expected additional tokens, got %d: %v", len(profiles[0].tokens), profiles[0].tokens)
	}
}

func TestFilterCandidates_EmptySourceURL(t *testing.T) {
	validator := &testSourceValidator{}

	now := time.Date(2026, time.February, 16, 10, 0, 0, 0, timeutil.KSTZone)
	date := time.Date(2026, time.February, 20, 12, 0, 0, 0, timeutil.KSTZone)

	candidates := []model.Candidate{
		{Title: "사쿠라 미코 event", EventStartDate: &date, Type: domain.MajorEventTypeEvent, SourceURL: ""},
		{Title: "사쿠라 미코 event2", EventStartDate: &date, Type: domain.MajorEventTypeEvent, SourceURL: "  "},
	}

	filtered := mustFilterCandidates(t, candidates, now, []string{testMemberMiko}, validator)
	if len(filtered) != 0 {
		t.Fatalf("expected 0 (empty sourceURL excluded), got %d", len(filtered))
	}
}

func TestFilterCandidates_CommunityWithoutCorroboration(t *testing.T) {
	validator := &testSourceValidator{}

	now := time.Date(2026, time.February, 16, 10, 0, 0, 0, timeutil.KSTZone)
	date := time.Date(2026, time.February, 20, 12, 0, 0, 0, timeutil.KSTZone)

	candidates := []model.Candidate{
		{
			Title:          "사쿠라 미코 event",
			Description:    "비공식 정보만 포함됨",
			EventStartDate: &date,
			Type:           domain.MajorEventTypeEvent,
			SourceURL:      "https://example.com/post/1",
		},
	}

	filtered := mustFilterCandidates(t, candidates, now, []string{testMemberMiko}, validator)
	if len(filtered) != 0 {
		t.Fatalf("expected 0 (community without corroboration excluded), got %d", len(filtered))
	}
}

func TestFilterCandidates_SortStability(t *testing.T) {
	validator := &testSourceValidator{}

	now := time.Date(2026, time.February, 16, 10, 0, 0, 0, timeutil.KSTZone)
	date1 := time.Date(2026, time.February, 18, 12, 0, 0, 0, timeutil.KSTZone)
	date2 := time.Date(2026, time.February, 20, 12, 0, 0, 0, timeutil.KSTZone)

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

	filtered := mustFilterCandidates(t, candidates, now, []string{testMemberMiko}, validator)
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

	now := time.Date(2026, time.February, 16, 10, 0, 0, 0, timeutil.KSTZone)
	date := time.Date(2026, time.February, 20, 12, 0, 0, 0, timeutil.KSTZone)

	candidates := []model.Candidate{
		{
			Title:          "사쿠라 미코 호시마치 스이세이 콜라보",
			Description:    "official collab",
			Members:        []string{testMemberMiko, testMemberSuisei},
			EventStartDate: &date,
			Type:           domain.MajorEventTypeEvent,
			SourceURL:      "https://hololive.hololivepro.com/events/collab1",
		},
	}

	filtered := mustFilterCandidates(t, candidates, now,
		[]string{testMemberMiko, testMemberSuisei}, validator)
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
