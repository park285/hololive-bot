package summarizer

import (
	"reflect"
	"testing"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/consensus"
)

func TestReviewVerdictSchema_Golden(t *testing.T) {
	t.Parallel()

	want := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"approved": map[string]any{"type": "boolean"},
			"issues": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"field":      map[string]any{"type": "string"},
						"item_index": map[string]any{"type": "integer"},
						"severity": map[string]any{
							"type": "string",
							"enum": []string{"critical", "warning", "info"},
						},
						"description": map[string]any{"type": "string"},
					},
					"required": []string{"field", "item_index", "severity", "description"},
				},
			},
			"confidence": map[string]any{
				"type":    "number",
				"minimum": 0.0,
				"maximum": 1.0,
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
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"period": map[string]any{
				"type": "string",
				"enum": []string{"weekly", "monthly"},
			},
			"headline": map[string]any{"type": "string"},
			"top_items": map[string]any{
				"type":     "array",
				"maxItems": 5,
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"member":     map[string]any{"type": "string"},
						"category":   map[string]any{"type": "string"},
						"title":      map[string]any{"type": "string"},
						"date_text":  map[string]any{"type": "string"},
						"summary":    map[string]any{"type": "string"},
						"source_url": map[string]any{"type": "string"},
					},
					"required": []string{"member", "category", "title", "date_text", "summary", "source_url"},
				},
			},
			"more_summary":  map[string]any{"type": "string"},
			"omitted_count": map[string]any{"type": "integer", "minimum": 0},
		},
		"required": []string{"period", "headline", "top_items", "more_summary", "omitted_count"},
	}

	if got := memberNewsSummarySchema(); !reflect.DeepEqual(got, want) {
		t.Fatalf("memberNewsSummarySchema() golden mismatch\n got: %#v\nwant: %#v", got, want)
	}
}
