package summarizer

import (
	"reflect"
	"testing"
)

func wantHighlightItemSchema() map[string]any {
	return map[string]any{
		wantKeyType:                 wantTypeObject,
		wantKeyAdditionalProperties: false,
		wantKeyProperties: map[string]any{
			wantFieldName: map[string]any{
				wantKeyType:        wantTypeString,
				wantKeyDescription: "행사명 — 일본어 원제 유지, 번역 금지",
			},
			wantFieldDate: map[string]any{
				wantKeyType:        wantTypeString,
				wantKeyDescription: "날짜 — M/D(요일) 또는 M/D(요일)~M/D(요일)",
			},
			wantFieldMembers: map[string]any{
				wantKeyType:        wantTypeString,
				wantKeyDescription: "참여 멤버 (졸업 멤버 제외, 8명 초과시 축약, 없으면 빈 문자열)",
			},
			wantFieldNote: map[string]any{
				wantKeyType:        wantTypeString,
				wantKeyDescription: "행사 한줄 설명 — 30자 이내, 사실적 한국어, 멤버명 반복 금지",
				wantKeyMaxLength:   30,
			},
			wantFieldLink: map[string]any{
				wantKeyType:        wantTypeString,
				wantKeyDescription: "입력 행사 link 그대로 전달",
			},
		},
		wantKeyRequired: []string{wantFieldName, wantFieldDate, wantFieldMembers, wantFieldNote, wantFieldLink},
	}
}

func wantOngoingItemSchema() map[string]any {
	return map[string]any{
		wantKeyType:                 wantTypeObject,
		wantKeyAdditionalProperties: false,
		wantKeyProperties: map[string]any{
			wantFieldName: map[string]any{
				wantKeyType:        wantTypeString,
				wantKeyDescription: "행사명",
			},
			wantFieldDate: map[string]any{
				wantKeyType:        wantTypeString,
				wantKeyDescription: "날짜 — M/D(요일)~M/D(요일)",
			},
			wantFieldNote: map[string]any{
				wantKeyType:        wantTypeString,
				wantKeyDescription: "행사 설명 — 30자 이내 한국어",
				wantKeyMaxLength:   30,
			},
			wantFieldLink: map[string]any{
				wantKeyType:        wantTypeString,
				wantKeyDescription: "입력 행사 link 그대로 전달",
			},
		},
		wantKeyRequired: []string{wantFieldName, wantFieldDate, wantFieldNote, wantFieldLink},
	}
}

func wantDiscoveredItemSchema() map[string]any {
	return map[string]any{
		wantKeyType:                 wantTypeObject,
		wantKeyAdditionalProperties: false,
		wantKeyProperties: map[string]any{
			wantFieldName: map[string]any{
				wantKeyType:        wantTypeString,
				wantKeyDescription: "행사명 — 공식 명칭 사용",
			},
			wantFieldDate: map[string]any{
				wantKeyType:        wantTypeString,
				wantKeyDescription: "날짜 — M/D(요일) 또는 M/D(요일)~M/D(요일)",
			},
			wantFieldNote: map[string]any{
				wantKeyType:        wantTypeString,
				wantKeyDescription: "행사 설명 — 30자 이내 한국어",
				wantKeyMaxLength:   30,
			},
			wantFieldSource: map[string]any{
				wantKeyType:        wantTypeString,
				wantKeyDescription: "출처 URL — 반드시 https:// 형식 전체 URL 또는 @계정명",
			},
		},
		wantKeyRequired: []string{wantFieldName, wantFieldDate, wantFieldNote, wantFieldSource},
	}
}

func wantReviewIssueSchema() map[string]any {
	return map[string]any{
		wantKeyType:                 wantTypeObject,
		wantKeyAdditionalProperties: false,
		wantKeyProperties: map[string]any{
			"field": map[string]any{
				wantKeyType: wantTypeString,
			},
			"item_index": map[string]any{
				wantKeyType: "integer",
			},
			"severity": map[string]any{
				wantKeyType: wantTypeString,
				"enum":      []string{severityCritical, severityWarning, severityInfo},
			},
			"description": map[string]any{
				wantKeyType: wantTypeString,
			},
		},
		wantKeyRequired: []string{"field", "item_index", "severity", "description"},
	}
}

func TestSummaryResponseSchema_Golden(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		wantKeyType:                 wantTypeObject,
		wantKeyAdditionalProperties: false,
		wantKeyProperties: map[string]any{
			"highlights": map[string]any{
				wantKeyType:        wantTypeArray,
				wantKeyDescription: "행사별 하이라이트 (날짜순 정렬)",
				"items":            wantHighlightItemSchema(),
			},
			"ongoing_events": map[string]any{
				wantKeyType:        wantTypeArray,
				wantKeyDescription: "기간 행사(팝업/카페/굿즈) 목록",
				"items":            wantOngoingItemSchema(),
			},
			"discovered_events": map[string]any{
				wantKeyType:        wantTypeArray,
				wantKeyDescription: "Google Search로 발견한 입력 목록에 없는 추가 이벤트 (최대 5건, 없으면 빈 배열)",
				"items":            wantDiscoveredItemSchema(),
			},
		},
		wantKeyRequired: []string{"highlights", "ongoing_events", "discovered_events"},
	}

	if got := summaryResponseSchema(); !reflect.DeepEqual(got, want) {
		t.Fatalf("summaryResponseSchema() golden mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestSummaryHighlightItemSchema_Golden(t *testing.T) {
	t.Parallel()

	if got := summaryHighlightItemSchema(); !reflect.DeepEqual(got, wantHighlightItemSchema()) {
		t.Fatalf("summaryHighlightItemSchema() golden mismatch\n got: %#v\nwant: %#v", got, wantHighlightItemSchema())
	}
}

func TestSummaryOngoingItemSchema_Golden(t *testing.T) {
	t.Parallel()

	if got := summaryOngoingItemSchema(); !reflect.DeepEqual(got, wantOngoingItemSchema()) {
		t.Fatalf("summaryOngoingItemSchema() golden mismatch\n got: %#v\nwant: %#v", got, wantOngoingItemSchema())
	}
}

func TestSummaryDiscoveredItemSchema_Golden(t *testing.T) {
	t.Parallel()

	if got := summaryDiscoveredItemSchema(); !reflect.DeepEqual(got, wantDiscoveredItemSchema()) {
		t.Fatalf("summaryDiscoveredItemSchema() golden mismatch\n got: %#v\nwant: %#v", got, wantDiscoveredItemSchema())
	}
}

func TestReviewSummarySchema_Golden(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		wantKeyType:                 wantTypeObject,
		wantKeyAdditionalProperties: false,
		wantKeyProperties: map[string]any{
			"approved": map[string]any{
				wantKeyType: "boolean",
			},
			"confidence": map[string]any{
				wantKeyType: "number",
				"minimum":   0,
				"maximum":   1,
			},
			"issues": map[string]any{
				wantKeyType: wantTypeArray,
				"items":     wantReviewIssueSchema(),
			},
		},
		wantKeyRequired: []string{"approved", "confidence", "issues"},
	}

	if got := reviewSummarySchema(); !reflect.DeepEqual(got, want) {
		t.Fatalf("reviewSummarySchema() golden mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestReviewIssueSchema_Golden(t *testing.T) {
	t.Parallel()

	if got := reviewIssueSchema(); !reflect.DeepEqual(got, wantReviewIssueSchema()) {
		t.Fatalf("reviewIssueSchema() golden mismatch\n got: %#v\nwant: %#v", got, wantReviewIssueSchema())
	}
}

func TestFinalOutputReviewSchema_Golden(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		wantKeyType:                 wantTypeObject,
		wantKeyAdditionalProperties: false,
		wantKeyProperties: map[string]any{
			"summary": map[string]any{
				wantKeyType:        wantTypeString,
				wantKeyDescription: "final deduplicated summary text",
			},
		},
		wantKeyRequired: []string{"summary"},
	}

	if got := finalOutputReviewSchema(); !reflect.DeepEqual(got, want) {
		t.Fatalf("finalOutputReviewSchema() golden mismatch\n got: %#v\nwant: %#v", got, want)
	}
}
