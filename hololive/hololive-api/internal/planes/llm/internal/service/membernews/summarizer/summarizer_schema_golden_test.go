package summarizer

import (
	"reflect"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/consensus"
)

// golden 기대값은 프로덕션 상수를 참조하지 않고 여기에 리터럴로 고정한다.
// 프로덕션 값이 바뀌면 기대값이 함께 따라가지 않고 테스트가 실패해야 한다.
const (
	goldenKeyType          = "type"
	goldenTypeString       = "string"
	goldenSeverityCritical = "critical"
	goldenSeverityWarning  = "warning"
	goldenSeverityInfo     = "info"
)

func TestReviewVerdictSchema_Golden(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		goldenKeyType:          "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"approved": map[string]any{goldenKeyType: "boolean"},
			"issues": map[string]any{
				goldenKeyType: "array",
				"items": map[string]any{
					goldenKeyType:          "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"field":      map[string]any{goldenKeyType: goldenTypeString},
						"item_index": map[string]any{goldenKeyType: "integer"},
						"severity": map[string]any{
							goldenKeyType: goldenTypeString,
							"enum":        []string{goldenSeverityCritical, goldenSeverityWarning, goldenSeverityInfo},
						},
						"description": map[string]any{goldenKeyType: goldenTypeString},
					},
					"required": []string{"field", "item_index", "severity", "description"},
				},
			},
			"confidence": map[string]any{
				goldenKeyType: "number",
				"minimum":     0.0,
				"maximum":     1.0,
			},
		},
		"required": []string{"approved", "issues", "confidence"},
	}

	if got := consensus.ReviewVerdictSchema(); !reflect.DeepEqual(got, want) {
		t.Fatalf("consensus.ReviewVerdictSchema() golden mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestMemberNewsSummarySchema_Golden(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		goldenKeyType:          "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"period": map[string]any{
				goldenKeyType: goldenTypeString,
				"enum":        []string{"weekly", "monthly"},
			},
			"headline": map[string]any{goldenKeyType: goldenTypeString},
			"top_items": map[string]any{
				goldenKeyType: "array",
				"maxItems":    5,
				"items": map[string]any{
					goldenKeyType:          "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"member":     map[string]any{goldenKeyType: goldenTypeString},
						"category":   map[string]any{goldenKeyType: goldenTypeString},
						"title":      map[string]any{goldenKeyType: goldenTypeString},
						"date_text":  map[string]any{goldenKeyType: goldenTypeString},
						"summary":    map[string]any{goldenKeyType: goldenTypeString},
						"source_url": map[string]any{goldenKeyType: goldenTypeString},
					},
					"required": []string{"member", "category", "title", "date_text", "summary", "source_url"},
				},
			},
			"more_summary":  map[string]any{goldenKeyType: goldenTypeString},
			"omitted_count": map[string]any{goldenKeyType: "integer", "minimum": 0},
		},
		"required": []string{"period", "headline", "top_items", "more_summary", "omitted_count"},
	}

	if got := memberNewsSummarySchema(); !reflect.DeepEqual(got, want) {
		t.Fatalf("memberNewsSummarySchema() golden mismatch\n got: %#v\nwant: %#v", got, want)
	}
}
