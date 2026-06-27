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

import "github.com/kapu/hololive-api/internal/planes/llm/internal/service/consensus"

type summaryResponse struct {
	Highlights       []eventHighlight  `json:"highlights"`
	OngoingEvents    []ongoingEvent    `json:"ongoing_events"`
	DiscoveredEvents []discoveredEvent `json:"discovered_events"`
}

type eventHighlight struct {
	Name    string `json:"name"`
	Date    string `json:"date"`
	Members string `json:"members"`
	Note    string `json:"note"`
	Link    string `json:"link"`
}

type ongoingEvent struct {
	Name string `json:"name"`
	Date string `json:"date"`
	Note string `json:"note"`
	Link string `json:"link"`
}

type discoveredEvent struct {
	Name   string `json:"name"`
	Date   string `json:"date"`
	Note   string `json:"note"`
	Source string `json:"source"`
}

func summaryResponseSchema() map[string]any {
	return consensus.ObjectSchema(
		map[string]any{
			"highlights": map[string]any{
				"type":        "array",
				"description": "행사별 하이라이트 (날짜순 정렬)",
				"items":       summaryHighlightItemSchema(),
			},
			"ongoing_events": map[string]any{
				"type":        "array",
				"description": "기간 행사(팝업/카페/굿즈) 목록",
				"items":       summaryOngoingItemSchema(),
			},
			"discovered_events": map[string]any{
				"type":        "array",
				"description": "Google Search로 발견한 입력 목록에 없는 추가 이벤트 (최대 5건, 없으면 빈 배열)",
				"items":       summaryDiscoveredItemSchema(),
			},
		},
		[]string{"highlights", "ongoing_events", "discovered_events"},
	)
}

func summaryHighlightItemSchema() map[string]any {
	return consensus.ObjectSchema(
		map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "행사명 — 일본어 원제 유지, 번역 금지",
			},
			"date": map[string]any{
				"type":        "string",
				"description": "날짜 — M/D(요일) 또는 M/D(요일)~M/D(요일)",
			},
			"members": map[string]any{
				"type":        "string",
				"description": "참여 멤버 (졸업 멤버 제외, 8명 초과시 축약, 없으면 빈 문자열)",
			},
			"note": map[string]any{
				"type":        "string",
				"description": "행사 한줄 설명 — 30자 이내, 사실적 한국어, 멤버명 반복 금지",
				"maxLength":   30,
			},
			"link": map[string]any{
				"type":        "string",
				"description": "입력 행사 link 그대로 전달",
			},
		},
		[]string{"name", "date", "members", "note", "link"},
	)
}

func summaryOngoingItemSchema() map[string]any {
	return consensus.ObjectSchema(
		map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "행사명",
			},
			"date": map[string]any{
				"type":        "string",
				"description": "날짜 — M/D(요일)~M/D(요일)",
			},
			"note": map[string]any{
				"type":        "string",
				"description": "행사 설명 — 30자 이내 한국어",
				"maxLength":   30,
			},
			"link": map[string]any{
				"type":        "string",
				"description": "입력 행사 link 그대로 전달",
			},
		},
		[]string{"name", "date", "note", "link"},
	)
}

func summaryDiscoveredItemSchema() map[string]any {
	return consensus.ObjectSchema(
		map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "행사명 — 공식 명칭 사용",
			},
			"date": map[string]any{
				"type":        "string",
				"description": "날짜 — M/D(요일) 또는 M/D(요일)~M/D(요일)",
			},
			"note": map[string]any{
				"type":        "string",
				"description": "행사 설명 — 30자 이내 한국어",
				"maxLength":   30,
			},
			"source": map[string]any{
				"type":        "string",
				"description": "출처 URL — 반드시 https:// 형식 전체 URL 또는 @계정명",
			},
		},
		[]string{"name", "date", "note", "source"},
	)
}
