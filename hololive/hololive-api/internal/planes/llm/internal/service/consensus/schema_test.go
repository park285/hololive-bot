package consensus

import (
	"reflect"
	"testing"
)

func TestObjectSchema(t *testing.T) {
	t.Parallel()

	props := map[string]any{"a": map[string]any{"type": "string"}}
	required := []string{"a"}
	got := ObjectSchema(props, required)
	want := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           props,
		"required":             required,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ObjectSchema mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestArraySchema(t *testing.T) {
	t.Parallel()

	items := map[string]any{"type": "string"}
	got := ArraySchema(items)
	want := map[string]any{
		"type":  "array",
		"items": items,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ArraySchema mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestTypeSchema(t *testing.T) {
	t.Parallel()

	for _, st := range []string{"string", "boolean", "integer", "number"} {
		got := TypeSchema(st)
		want := map[string]any{"type": st}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("TypeSchema(%q) mismatch\n got: %#v\nwant: %#v", st, got, want)
		}
	}
}

func TestEnumSchema(t *testing.T) {
	t.Parallel()

	values := []string{"critical", "warning", "info"}
	got := EnumSchema("string", values)
	want := map[string]any{
		"type": "string",
		"enum": values,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("EnumSchema mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestNumberRangeSchema(t *testing.T) {
	t.Parallel()

	got := NumberRangeSchema(0.0, 1.0)
	want := map[string]any{
		"type":    "number",
		"minimum": 0.0,
		"maximum": 1.0,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NumberRangeSchema mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestReviewVerdictSchema(t *testing.T) {
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

	if got := ReviewVerdictSchema(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ReviewVerdictSchema mismatch\n got: %#v\nwant: %#v", got, want)
	}
}
