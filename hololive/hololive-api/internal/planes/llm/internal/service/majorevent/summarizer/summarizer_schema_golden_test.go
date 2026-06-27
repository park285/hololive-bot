package summarizer

import (
	"reflect"
	"testing"
)

func TestSummaryResponseSchema_Golden(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"highlights": map[string]any{
				"type":        "array",
				"description": "행사별 하이라이트 (날짜순 정렬)",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
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
					"required": []string{"name", "date", "members", "note", "link"},
				},
			},
			"ongoing_events": map[string]any{
				"type":        "array",
				"description": "기간 행사(팝업/카페/굿즈) 목록",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
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
					"required": []string{"name", "date", "note", "link"},
				},
			},
			"discovered_events": map[string]any{
				"type":        "array",
				"description": "Google Search로 발견한 입력 목록에 없는 추가 이벤트 (최대 5건, 없으면 빈 배열)",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
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
					"required": []string{"name", "date", "note", "source"},
				},
			},
		},
		"required": []string{"highlights", "ongoing_events", "discovered_events"},
	}

	if got := summaryResponseSchema(); !reflect.DeepEqual(got, want) {
		t.Fatalf("summaryResponseSchema() golden mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSummaryHighlightItemSchema_Golden(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
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
		"required": []string{"name", "date", "members", "note", "link"},
	}

	if got := summaryHighlightItemSchema(); !reflect.DeepEqual(got, want) {
		t.Fatalf("summaryHighlightItemSchema() golden mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSummaryOngoingItemSchema_Golden(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
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
		"required": []string{"name", "date", "note", "link"},
	}

	if got := summaryOngoingItemSchema(); !reflect.DeepEqual(got, want) {
		t.Fatalf("summaryOngoingItemSchema() golden mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSummaryDiscoveredItemSchema_Golden(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
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
		"required": []string{"name", "date", "note", "source"},
	}

	if got := summaryDiscoveredItemSchema(); !reflect.DeepEqual(got, want) {
		t.Fatalf("summaryDiscoveredItemSchema() golden mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestReviewSummarySchema_Golden(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"approved": map[string]any{
				"type": "boolean",
			},
			"confidence": map[string]any{
				"type":    "number",
				"minimum": 0,
				"maximum": 1,
			},
			"issues": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"field": map[string]any{
							"type": "string",
						},
						"item_index": map[string]any{
							"type": "integer",
						},
						"severity": map[string]any{
							"type": "string",
							"enum": []string{"critical", "warning", "info"},
						},
						"description": map[string]any{
							"type": "string",
						},
					},
					"required": []string{"field", "item_index", "severity", "description"},
				},
			},
		},
		"required": []string{"approved", "confidence", "issues"},
	}

	if got := reviewSummarySchema(); !reflect.DeepEqual(got, want) {
		t.Fatalf("reviewSummarySchema() golden mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestReviewIssueSchema_Golden(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"field": map[string]any{
				"type": "string",
			},
			"item_index": map[string]any{
				"type": "integer",
			},
			"severity": map[string]any{
				"type": "string",
				"enum": []string{"critical", "warning", "info"},
			},
			"description": map[string]any{
				"type": "string",
			},
		},
		"required": []string{"field", "item_index", "severity", "description"},
	}

	if got := reviewIssueSchema(); !reflect.DeepEqual(got, want) {
		t.Fatalf("reviewIssueSchema() golden mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestFinalOutputReviewSchema_Golden(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"summary": map[string]any{
				"type":        "string",
				"description": "final deduplicated summary text",
			},
		},
		"required": []string{"summary"},
	}

	if got := finalOutputReviewSchema(); !reflect.DeepEqual(got, want) {
		t.Fatalf("finalOutputReviewSchema() golden mismatch\n got: %#v\nwant: %#v", got, want)
	}
}
