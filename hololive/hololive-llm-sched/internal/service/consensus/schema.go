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

package consensus

func ObjectSchema(properties map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           properties,
		"required":             required,
	}
}

func ArraySchema(items map[string]any) map[string]any {
	return map[string]any{
		"type":  "array",
		"items": items,
	}
}

func TypeSchema(schemaType string) map[string]any {
	return map[string]any{"type": schemaType}
}

func EnumSchema(schemaType string, values []string) map[string]any {
	return map[string]any{
		"type": schemaType,
		"enum": values,
	}
}

func NumberRangeSchema(minimum, maximum float64) map[string]any {
	return map[string]any{
		"type":    "number",
		"minimum": minimum,
		"maximum": maximum,
	}
}

func ReviewVerdictSchema() map[string]any {
	return ObjectSchema(
		map[string]any{
			"approved":   TypeSchema("boolean"),
			"issues":     ArraySchema(reviewVerdictIssueSchema()),
			"confidence": NumberRangeSchema(0.0, 1.0),
		},
		[]string{"approved", "issues", "confidence"},
	)
}

func reviewVerdictIssueSchema() map[string]any {
	return ObjectSchema(
		map[string]any{
			"field":       TypeSchema("string"),
			"item_index":  TypeSchema("integer"),
			"severity":    EnumSchema("string", []string{"critical", "warning", "info"}),
			"description": TypeSchema("string"),
		},
		[]string{"field", "item_index", "severity", "description"},
	)
}
