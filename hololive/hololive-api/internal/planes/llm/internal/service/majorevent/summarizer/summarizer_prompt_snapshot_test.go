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
	"strings"
	"testing"
)

type promptSnapshot struct {
	orderedSnippets []string
	sections        []promptSectionSnapshot
}

type promptSectionSnapshot struct {
	name     string
	startTag string
	endTag   string
	snippets []string
}

func TestSystemPrompt_WeeklySnapshotContainsCriticalSections(t *testing.T) {
	prompt, err := getSystemPrompt(SummaryTypeWeekly)
	if err != nil {
		t.Fatalf("getSystemPrompt 실패: %v", err)
	}

	assertPromptCriticalSections(t, prompt, weeklyPromptSnapshot())
}

func TestSystemPrompt_MonthlySnapshotContainsCriticalSections(t *testing.T) {
	prompt, err := getSystemPrompt(SummaryTypeMonthly)
	if err != nil {
		t.Fatalf("getSystemPrompt 실패: %v", err)
	}

	assertPromptCriticalSections(t, prompt, monthlyPromptSnapshot())
}

func TestBuildPromptAssetVersion_ChangesWhenAssetsChange(t *testing.T) {
	base := map[string][]byte{
		"prompts/domain_context_part1.tmpl":  []byte("part1"),
		"prompts/domain_context_part2.tmpl":  []byte("part2"),
		"prompts/weekly_system_prompt.tmpl":  []byte("weekly"),
		"prompts/monthly_system_prompt.tmpl": []byte("monthly"),
		"graduated_members.json":             []byte(`{"graduated":{}}`),
	}
	changed := map[string][]byte{
		"prompts/domain_context_part1.tmpl":  []byte("part1"),
		"prompts/domain_context_part2.tmpl":  []byte("part2"),
		"prompts/weekly_system_prompt.tmpl":  []byte("weekly changed"),
		"prompts/monthly_system_prompt.tmpl": []byte("monthly"),
		"graduated_members.json":             []byte(`{"graduated":{}}`),
	}

	baseVersion := buildPromptAssetVersion(base)
	changedVersion := buildPromptAssetVersion(changed)

	if baseVersion == changedVersion {
		t.Fatalf("prompt asset version should change when embedded assets change: %q", baseVersion)
	}
}

func weeklyPromptSnapshot() promptSnapshot {
	return promptSnapshot{
		orderedSnippets: []string{
			`<scope_fence priority="HARD">`,
			`<member_filter>`,
			`<translation_guide>`,
			`<example>`,
		},
		sections: []promptSectionSnapshot{
			{
				name:     "scope_fence",
				startTag: `<scope_fence priority="HARD">`,
				endTag:   `</scope_fence>`,
				snippets: []string{
					`- Account for ALL input events — each must appear in EITHER highlights OR ongoing_events (none dropped)`,
					`- Apply the same member_filter and translation_guide rules`,
					`<date_authority priority="HARD">`,
					`- ALWAYS use dates from the input event's "date" field — NEVER copy dates from examples`,
					`- NOTE: input format is "YYYY년 M월 D일", output format is "M/D(요일)" — these are different pipeline stages`,
				},
			},
			{
				name:     "member_filter",
				startTag: `<member_filter>`,
				endTag:   `</member_filter>`,
				snippets: []string{
					`Remove graduated or retired members from the "members" output field.`,
					`Known graduated/retired members (reverse-chronological):`,
					`Gawr Gura (May 2025)`,
					`If >8 participating members after filtering: abbreviate as "JP/EN 다수" or "전체 참여" — do NOT list individually.`,
					`Exception: unit events (holoX, ReGLOSS, FLOW GLOW, etc.) — always list all unit members.`,
				},
			},
			{
				name:     "translation_guide",
				startTag: `<translation_guide>`,
				endTag:   `</translation_guide>`,
				snippets: []string{
					`Output language: Korean (한국어)`,
					`- 配信(はいしん) → "방송" or "스트리밍" (NEVER "배신" — means betrayal)`,
					`- ライブ / LIVE → "라이브" | お披露目 → "공개" (e.g., 3Dお披露目 → "3D 공개")`,
					`- Keep Japanese event titles in original form — do NOT translate`,
				},
			},
			{
				name:     "weekly_example",
				startTag: `<example>`,
				endTag:   `</example>`,
				snippets: []string{
					`Input: PROJECT ALPHA Solo Live (7/12, メンバーA, link: https://hololivepro.com/events/111) + Unit BETA Live (7/18~7/20, メンバーB, メンバーC, メンバーD(卒業済), link: https://hololivepro.com/events/222) + POP UP STORE (7/1~7/14, link: https://hololivepro.com/news/333)`,
					`{"name":"PROJECT ALPHA Solo Live","date":"7/12(토)","members":"メンバーA","note":"솔로 라이브 공연","link":"https://hololivepro.com/events/111"}`,
					`{"name":"Unit BETA Live","date":"7/18(수)~7/20(금)","members":"メンバーB, メンバーC","note":"유닛 첫 단독 라이브","link":"https://hololivepro.com/events/222"}`,
					`{"name":"POP UP STORE","date":"7/1(화)~7/14(월)","note":"팝업 스토어 진행 중","link":"https://hololivepro.com/news/333"}`,
					`{"name":"メンバーE 生誕祭2027","date":"7/15(화)","note":"생일 기념 라이브","source":"hololivepro.com/events"}`,
					`Key points: メンバーD(卒業済) removed, discovered_events from Google Search with source, dates include weekday, link copied from input`,
				},
			},
		},
	}
}

func monthlyPromptSnapshot() promptSnapshot {
	return promptSnapshot{
		orderedSnippets: []string{
			`<scope_fence priority="HARD">`,
			`<member_filter>`,
			`<translation_guide>`,
			`<example>`,
		},
		sections: []promptSectionSnapshot{
			{
				name:     "scope_fence",
				startTag: `<scope_fence priority="HARD">`,
				endTag:   `</scope_fence>`,
				snippets: []string{
					`- Account for ALL input events — each must appear in EITHER highlights OR ongoing_events (none dropped)`,
					`- Apply the same member_filter and translation_guide rules`,
					`<date_authority priority="HARD">`,
					`- ALWAYS use dates from the input event's "date" field — NEVER copy dates from examples`,
					`- NOTE: input format is "YYYY년 M월 D일", output format is "M/D(요일)" — these are different pipeline stages`,
				},
			},
			{
				name:     "member_filter",
				startTag: `<member_filter>`,
				endTag:   `</member_filter>`,
				snippets: []string{
					`Remove graduated or retired members from the "members" output field.`,
					`Known graduated/retired members (reverse-chronological):`,
					`Gawr Gura (May 2025)`,
					`If >8 participating members after filtering: abbreviate as "JP/EN 다수" or "전체 참여" — do NOT list individually.`,
					`Exception: unit events (holoX, ReGLOSS, FLOW GLOW, etc.) — always list all unit members.`,
				},
			},
			{
				name:     "translation_guide",
				startTag: `<translation_guide>`,
				endTag:   `</translation_guide>`,
				snippets: []string{
					`Output language: Korean (한국어)`,
					`- 配信(はいしん) → "방송" or "스트리밍" (NEVER "배신" — means betrayal)`,
					`- ライブ / LIVE → "라이브" | お披露目 → "공개" (e.g., 3Dお披露目 → "3D 공개")`,
					`- Keep Japanese event titles in original form — do NOT translate`,
				},
			},
			{
				name:     "monthly_example",
				startTag: `<example>`,
				endTag:   `</example>`,
				snippets: []string{
					`Input: GRAND EXPO + 9th fes (8/5~8/7, 30+ members, link: https://hololivepro.com/events/expo2) + メンバーF/メンバーG Concert (8/27~8/28, link: https://hololivepro.com/events/444) + CD Release (8/24, メンバーH, link: https://hololivepro.com/events/cd2)`,
					`{"name":"GRAND EXPO 2027 & 9th fes.","date":"8/5(화)~8/7(목)","members":"JP/EN 다수","note":"3일간 엑스포 + 전체 라이브","link":"https://hololivepro.com/events/expo2"}`,
					`{"name":"メンバーH シングルCDリリース","date":"8/24(일)","members":"メンバーH","note":"애니메이션 타이업 싱글 발매","link":"https://hololivepro.com/events/cd2"}`,
					`{"name":"メンバーF / メンバーG 1st Concert","date":"8/27(수)~8/28(목)","members":"メンバーF, メンバーG","note":"첫 유닛 콘서트","link":"https://hololivepro.com/events/444"}`,
					`"discovered_events":[]}`,
					`Key points: large group abbreviated to "JP/EN 다수", discovered_events empty when no additional events found, link copied from input`,
				},
			},
		},
	}
}

func assertPromptCriticalSections(t *testing.T, prompt string, snapshot promptSnapshot) {
	t.Helper()
	assertPromptContainsOrderedSnippets(t, prompt, snapshot.orderedSnippets)
	for _, section := range snapshot.sections {
		block := extractPromptSection(t, prompt, section.name, section.startTag, section.endTag)
		for _, snippet := range section.snippets {
			assertContains(t, block, snippet)
		}
	}
}

func assertPromptContainsOrderedSnippets(t *testing.T, prompt string, snippets []string) {
	t.Helper()
	searchFrom := 0
	for _, snippet := range snippets {
		idx := strings.Index(prompt[searchFrom:], snippet)
		if idx == -1 {
			t.Fatalf("prompt missing ordered snippet %q", snippet)
		}
		searchFrom += idx + len(snippet)
	}
}

func extractPromptSection(t *testing.T, prompt, name, startTag, endTag string) string {
	t.Helper()
	start := strings.Index(prompt, startTag)
	if start == -1 {
		t.Fatalf("prompt missing %s start tag %q", name, startTag)
	}
	end := strings.Index(prompt[start:], endTag)
	if end == -1 {
		t.Fatalf("prompt missing %s end tag %q", name, endTag)
	}
	end += start + len(endTag)
	return prompt[start:end]
}
