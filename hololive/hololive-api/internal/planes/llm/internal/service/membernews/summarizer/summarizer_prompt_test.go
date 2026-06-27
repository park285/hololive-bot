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

package summarizer

import (
	"testing"
	"time"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/internal/model"
)

func promptFixtureInput() *model.SummarizeInput {
	return &model.SummarizeInput{
		Period:      model.PeriodWeekly,
		Now:         time.Date(2026, 2, 16, 10, 0, 0, 0, model.KST),
		RoomMembers: []string{"호시마치 스이세이", "사쿠라 미코"},
		Candidates: []model.FilteredCandidate{
			{
				Candidate: model.Candidate{
					Title:       "EXPO",
					Description: "official news",
				},
				EffectiveDate: time.Date(2026, 2, 20, 12, 0, 0, 0, model.KST),
				MemberText:    "사쿠라 미코",
				Category:      model.CategoryEvent,
				SourceTier:    model.SourceTierOfficial,
				SourceURL:     "https://hololive.hololivepro.com/news/1",
			},
			{
				Candidate: model.Candidate{
					Title:       "SUISEI LIVE",
					Description: "official event",
				},
				EffectiveDate: time.Date(2026, 2, 21, 12, 0, 0, 0, model.KST),
				MemberText:    "호시마치 스이세이",
				Category:      model.CategorySoloLive,
				SourceTier:    model.SourceTierOfficial,
				SourceURL:     "https://hololive.hololivepro.com/news/2",
			},
		},
	}
}

func TestBuildMemberNewsUserPrompt_SnapshotNoSearchContext(t *testing.T) {
	const want = `today=2026-02-16T10:00:00+09:00
period=weekly
room_members=사쿠라 미코, 호시마치 스이세이
candidate_events=[{"member":"사쿠라 미코","category":"event","title":"EXPO","date":"2026-02-20","source_url":"https://hololive.hololivepro.com/news/1","source_tier":"official","summary":"official news"},{"member":"호시마치 스이세이","category":"solo_live","title":"SUISEI LIVE","date":"2026-02-21","source_url":"https://hololive.hololivepro.com/news/2","source_tier":"official","summary":"official event"}]
Return only schema JSON.`

	got := buildMemberNewsUserPrompt(promptFixtureInput(), "")
	if got != want {
		t.Fatalf("buildMemberNewsUserPrompt snapshot mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestBuildMemberNewsUserPrompt_SnapshotWithSearchContext(t *testing.T) {
	const want = `today=2026-02-16T10:00:00+09:00
period=weekly
room_members=사쿠라 미코, 호시마치 스이세이
candidate_events=[{"member":"사쿠라 미코","category":"event","title":"EXPO","date":"2026-02-20","source_url":"https://hololive.hololivepro.com/news/1","source_tier":"official","summary":"official news"},{"member":"호시마치 스이세이","category":"solo_live","title":"SUISEI LIVE","date":"2026-02-21","source_url":"https://hololive.hololivepro.com/news/2","source_tier":"official","summary":"official event"}]
exa_search_context=ctx body
Return only schema JSON.`

	got := buildMemberNewsUserPrompt(promptFixtureInput(), "ctx body")
	if got != want {
		t.Fatalf("buildMemberNewsUserPrompt snapshot mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestBuildMemberNewsUserPrompt_CandidatesMatchBuildPromptCandidates(t *testing.T) {
	input := promptFixtureInput()
	candidates := buildPromptCandidates(input)
	if len(candidates) != len(input.Candidates) {
		t.Fatalf("expected %d candidates, got %d", len(input.Candidates), len(candidates))
	}
	for i := range input.Candidates {
		src := &input.Candidates[i]
		got := candidates[i]
		if got.Member != src.MemberText ||
			got.Category != string(src.Category) ||
			got.Title != src.Candidate.Title ||
			got.Date != src.EffectiveDate.In(kst).Format("2006-01-02") ||
			got.SourceURL != src.SourceURL ||
			got.SourceTier != string(src.SourceTier) ||
			got.Summary != src.Candidate.Description {
			t.Fatalf("candidate %d mapping mismatch: %+v", i, got)
		}
	}
}
