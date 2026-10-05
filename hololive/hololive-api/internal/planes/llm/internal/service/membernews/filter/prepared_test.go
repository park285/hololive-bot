package filter

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// 최적화 이전의 eager 정규화 결과를 독립적으로 계산하여 값과 stable 순서를 비교한다.
func eagerFilterCandidates(ctx context.Context, candidates []model.Candidate, period model.Period, now time.Time, roomMembers []string, membersData domain.MemberDataProvider, validator model.SourceURLValidator) ([]model.FilteredCandidate, error) {
	dated := applyPeriodFilter(candidates, period, now)

	profiles, err := buildMemberProfiles(ctx, roomMembers, membersData)
	if err != nil {
		return nil, err
	}

	result := make([]model.FilteredCandidate, 0, len(dated))

	for i := range dated {
		item := &dated[i]
		body := stringutil.NormalizeKey(item.candidate.Title + " " + item.candidate.Description)
		tokens := buildCandidateMemberTokenSet(item.candidate.Members)
		matched := make([]string, 0)
		seen := make(map[string]struct{})

		for _, profile := range profiles {
			if profileMatchesCandidate(profile, tokens, body) {
				matched = appendUniqueMatchedMember(matched, seen, profile.display)
			}
		}

		if len(matched) == 0 {
			continue
		}

		tier, sourceURL, ok := resolveSource(&item.candidate, validator)
		if !ok {
			continue
		}

		result = append(result, model.FilteredCandidate{
			Candidate: item.candidate, EffectiveDate: item.date, MatchedMembers: matched,
			MemberText: formatMemberText(matched), Category: classifyCategory(&item.candidate),
			SourceTier: tier, SourceURL: sourceURL,
		})
	}

	slices.SortStableFunc(result, compareFilteredCandidate)

	return result, nil
}

func performanceFilterFixture(count, descriptionRepeats int, now time.Time) []model.Candidate {
	cs := make([]model.Candidate, count)
	for i := range cs {
		cs[i] = model.Candidate{
			ID:          i + 1,
			Type:        domain.MajorEventTypeNews,
			Title:       fmt.Sprintf("미코 행사 %04d", i),
			Description: strings.Repeat("미코 공식 기사 ", descriptionRepeats),
			Members:     []string{"미코"},
			PubDate:     new(now),
			SourceURL:   fmt.Sprintf("https://hololive.hololivepro.com/%d", i),
		}
	}

	return cs
}

func TestPreparedCandidatesPreserveFullValuesAndOrder(t *testing.T) {
	now := time.Date(2026, time.October, 2, 12, 0, 0, 0, kst)
	member := &domain.Member{Name: "Sakura Miko", NameKo: "사쿠라 미코", NameJa: "さくらみこ", Aliases: &domain.Aliases{Ko: []string{"미코", "사쿠라미코"}, Ja: []string{"みこ"}}}
	provider := &mockMemberDataForFilter{byName: map[string]*domain.Member{"사쿠라 미코": member}, byAlias: map[string]*domain.Member{"미코": member, "사쿠라미코": member}}
	titles := []string{"미코 Birthday Live", "さくらみこ solo live", "みこ collaboration", "사쿠라-미코 굿즈", "무관 행사", "동률 행사"}
	bodies := []string{"미코 공식 기사", "사쿠라미코 생일", "\t미코	官方☆ー", "미코 https://hololivepro.com/x", "solo live 단독 라이브", ""}
	urls := []string{"https://hololivepro.com/a", "https://prtimes.jp/b", "https://example.com/c", "", " ::invalid:: ", " https://hololivepro.com/trim "}
	memberSets := [][]string{{"미코"}, {"Sakura_Miko"}, {"아쿠아"}, nil, {"", "미코", "미코"}}
	dates := []*time.Time{nil, new(now), new(now.AddDate(0, 0, -9)), new(now.AddDate(0, 0, 23)), new(time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC))}
	cs := make([]model.Candidate, 0, 150)

	for i := range 150 {
		typ := domain.MajorEventTypeNews

		if i%2 == 0 {
			typ = domain.MajorEventTypeEvent
		}

		cs = append(cs, model.Candidate{ID: i + 1, Type: typ, Title: titles[i%len(titles)], Description: bodies[(i/3)%len(bodies)], Members: memberSets[(i/2)%len(memberSets)], PubDate: dates[i%len(dates)], EventStartDate: dates[(i/7)%len(dates)], SourceURL: urls[(i/5)%len(urls)]})
	}

	// 같은 날짜·분류·제목이며 출처가 다른 후보도 원래 입력 순서를 보존합니다.
	cs = append(cs, model.Candidate{ID: 1001, Type: domain.MajorEventTypeNews, Title: "미코 동률 행사", PubDate: new(now), SourceURL: "https://hololivepro.com/tie-z"}, model.Candidate{ID: 1002, Type: domain.MajorEventTypeNews, Title: "미코 동률 행사", PubDate: new(now), SourceURL: "https://hololivepro.com/tie-a"})

	cases := 0

	for _, period := range []model.Period{model.PeriodWeekly, model.PeriodMonthly, "이번달"} {
		prepared := PrepareCandidates(cs, period, now)

		for _, members := range [][]string{{"미코"}, {"사쿠라 미코", "아쿠아"}, {"미코", "사쿠라미코", "미코"}, {"未知", " "}, nil} {
			for _, data := range []domain.MemberDataProvider{nil, provider} {
				for _, validator := range []model.SourceURLValidator{nil, &testSourceValidator{}} {
					want, wantErr := eagerFilterCandidates(t.Context(), cs, period, now, members, data, validator)
					got, gotErr := prepared.Filter(t.Context(), members, data, validator)

					if wantErr != nil || gotErr != nil || !reflect.DeepEqual(want, got) {
						t.Fatalf("전체값 또는 순서 불일치 period=%s members=%v provider=%t validator=%t", period, members, data != nil, validator != nil)
					}

					lazy, lazyErr := FilterCandidates(t.Context(), cs, period, now, members, data, validator)
					if lazyErr != nil || !reflect.DeepEqual(want, lazy) {
						t.Fatalf("lazy 전체값 또는 순서 불일치 period=%s members=%v provider=%t validator=%t", period, members, data != nil, validator != nil)
					}

					cases++
				}
			}
		}
	}

	t.Logf("metadata_equality input_candidates=%d cases=%d prototypes=prepared,lazy comparison=reflect.DeepEqual full_values_and_order=true", len(cs), cases)
}

func TestExactMemberTokensBypassBodyNormalization(t *testing.T) {
	profiles := []memberProfile{{display: "Miko", tokens: []string{"other", "miko"}}, {display: "Suisei", tokens: []string{"suisei"}}}
	calls := 0
	body := func() string { calls++; return "suisei" }
	got := matchPreparedMembers(profiles, map[string]struct{}{"miko": {}, "suisei": {}}, body)

	if !slices.Equal(got, []string{"Miko", "Suisei"}) || calls != 0 {
		t.Fatalf("exact matches=%v body calls=%d", got, calls)
	}

	got = matchPreparedMembers(profiles, map[string]struct{}{"miko": {}}, body)
	if !slices.Equal(got, []string{"Miko", "Suisei"}) || calls != 1 {
		t.Fatalf("mixed matches=%v body calls=%d", got, calls)
	}
}

func TestPreparedCandidatesOwnInputAndRoomOutput(t *testing.T) {
	now := time.Date(2026, time.October, 2, 12, 0, 0, 0, kst)
	candidates := performanceFilterFixture(2, 4, now)
	prepared := PrepareCandidates(candidates, model.PeriodWeekly, now)

	candidates[0].Members[0] = "changed input"
	*candidates[0].PubDate = now.AddDate(-1, 0, 0)

	var wg sync.WaitGroup

	for range 5 {
		wg.Go(func() {
			for range 10 {
				got, err := prepared.Filter(t.Context(), []string{"미코", "본문으로만 찾는 멤버"}, nil, nil)
				if err != nil || len(got) != 2 || got[0].Candidate.Members[0] != "미코" || !got[0].Candidate.PubDate.Equal(now) {
					t.Errorf("shared candidate changed: %#v", got)

					return
				}

				got[0].Candidate.Members[0] = "changed output"
				*got[0].Candidate.PubDate = now.AddDate(1, 0, 0)
				got[0].MatchedMembers[0] = "changed match"
			}
		})
	}

	wg.Wait()
}

func benchmarkMetadataBatch(b *testing.B, candidates []model.Candidate, now time.Time, variant string) {
	b.Helper()

	var prepared *PreparedCandidates

	if variant == "prepared" {
		prepared = PrepareCandidates(candidates, model.PeriodWeekly, now)
	}

	run := func() ([]model.FilteredCandidate, error) {
		switch variant {
		case "prepared":
			return prepared.Filter(b.Context(), []string{"미코"}, nil, nil)
		case "lazy":
			return FilterCandidates(b.Context(), candidates, model.PeriodWeekly, now, []string{"미코"}, nil, nil)
		default:
			return eagerFilterCandidates(b.Context(), candidates, model.PeriodWeekly, now, []string{"미코"}, nil, nil)
		}
	}

	var wg sync.WaitGroup

	for range 5 {
		wg.Go(func() {
			for range 4 {
				if got, err := run(); err != nil || len(got) != 1000 {
					b.Errorf("candidate count=%d err=%v", len(got), err)
				}
			}
		})
	}

	wg.Wait()
}

func BenchmarkCandidateMetadata(b *testing.B) {
	now := time.Date(2026, time.October, 2, 12, 0, 0, 0, kst)
	candidates := performanceFilterFixture(1000, 64, now)

	for _, variant := range []string{"eager", "lazy", "prepared"} {
		b.Run(variant, func(b *testing.B) {
			b.ReportAllocs()

			for b.Loop() {
				benchmarkMetadataBatch(b, candidates, now, variant)
			}
		})
	}
}

func TestPreparedCandidatesObserveAliasProfileForEachRoom(t *testing.T) {
	now := time.Date(2026, time.October, 2, 12, 0, 0, 0, kst)
	prepared := PrepareCandidates(performanceFilterFixture(1, 4, now), model.PeriodWeekly, now)
	member := &domain.Member{NameKo: "이전 이름", Aliases: &domain.Aliases{Ko: []string{"미코"}}}
	provider := &mockMemberDataForFilter{byAlias: map[string]*domain.Member{"미코": member}}
	first, firstErr := prepared.Filter(t.Context(), []string{"미코"}, provider, nil)

	member.NameKo = "다음 이름"

	second, secondErr := prepared.Filter(t.Context(), []string{"미코"}, provider, nil)

	if firstErr != nil || secondErr != nil || len(first) != 1 || len(second) != 1 || first[0].MemberText != "이전 이름" || second[0].MemberText != "다음 이름" {
		t.Fatalf("profiles were frozen: first=%v second=%v", first, second)
	}
}
