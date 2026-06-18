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

package llm

import (
	"testing"

	"github.com/openai/openai-go/v3/responses"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractResponsesOutputTextWithDiagnostics(t *testing.T) {
	t.Parallel()

	t.Run("nil response returns empty output error", func(t *testing.T) {
		t.Parallel()

		got, err := extractResponsesOutputTextWithDiagnostics(nil)

		assert.Empty(t, got)
		require.ErrorIs(t, err, errOpenAIEmptyOutput)
	})

	t.Run("response with text returns trimmed text", func(t *testing.T) {
		t.Parallel()

		got, err := extractResponsesOutputTextWithDiagnostics(&responses.Response{
			Status: responses.ResponseStatusCompleted,
			Output: []responses.ResponseOutputItemUnion{
				responseDiagnosticsOutputTextItem("  {\"summary\":\"ok\"}  "),
			},
		})

		require.NoError(t, err)
		assert.Equal(t, `{"summary":"ok"}`, got)
	})

	t.Run("empty text builds diagnostic", func(t *testing.T) {
		t.Parallel()

		got, err := extractResponsesOutputTextWithDiagnostics(&responses.Response{
			Status: responses.ResponseStatusIncomplete,
			IncompleteDetails: responses.ResponseIncompleteDetails{
				Reason: "max_output_tokens",
			},
			Output: []responses.ResponseOutputItemUnion{
				responseDiagnosticsRefusalItem("policy refusal"),
			},
		})

		assert.Empty(t, got)
		require.ErrorIs(t, err, errOpenAIEmptyOutput)
		assert.Contains(t, err.Error(), "status=incomplete")
		assert.Contains(t, err.Error(), "incomplete_reason=max_output_tokens")
		assert.Contains(t, err.Error(), "refusal=true")
		assert.Contains(t, err.Error(), "output=message/completed")
		assert.NotContains(t, err.Error(), "policy refusal")
	})
}

func TestDescribeResponsesOutput(t *testing.T) {
	t.Parallel()

	t.Run("nil returns empty", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, describeResponsesOutput(nil))
	})

	t.Run("includes status incomplete details and output types", func(t *testing.T) {
		t.Parallel()

		got := describeResponsesOutput(&responses.Response{
			Status: responses.ResponseStatusIncomplete,
			IncompleteDetails: responses.ResponseIncompleteDetails{
				Reason: "max_output_tokens",
			},
			Output: []responses.ResponseOutputItemUnion{
				{
					Type:   "message",
					Status: "incomplete",
				},
				{
					Type: "web_search_call",
				},
			},
		})

		assert.Contains(t, got, "status=incomplete")
		assert.Contains(t, got, "incomplete_reason=max_output_tokens")
		assert.Contains(t, got, "output=message/incomplete,web_search_call")
	})
}

func TestDescribeResponseOutputItemType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		item responses.ResponseOutputItemUnion
		want string
	}{
		{
			name: "empty type returns unknown",
			item: responses.ResponseOutputItemUnion{},
			want: "unknown",
		},
		{
			name: "includes status",
			item: responses.ResponseOutputItemUnion{
				Type:   "message",
				Status: "completed",
			},
			want: "message/completed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, describeResponseOutputItemType(&tt.item))
		})
	}
}

func TestResponseOutputItemRefusal(t *testing.T) {
	t.Parallel()

	t.Run("non-message type returns empty", func(t *testing.T) {
		t.Parallel()

		item := responses.ResponseOutputItemUnion{
			Type: "function_call",
			Content: []responses.ResponseOutputMessageContentUnion{
				{
					Type:    "refusal",
					Refusal: "policy refusal",
				},
			},
		}
		got := responseOutputItemRefusal(&item)

		assert.Empty(t, got)
	})

	t.Run("message with refusal content returns refusal text", func(t *testing.T) {
		t.Parallel()

		item := responseDiagnosticsRefusalItem("  policy refusal  ")
		got := responseOutputItemRefusal(&item)

		assert.Equal(t, "policy refusal", got)
	})

	t.Run("message without refusal content returns empty", func(t *testing.T) {
		t.Parallel()

		item := responseDiagnosticsOutputTextItem("ok")
		got := responseOutputItemRefusal(&item)

		assert.Empty(t, got)
	})
}

func responseDiagnosticsOutputTextItem(text string) responses.ResponseOutputItemUnion {
	return responses.ResponseOutputItemUnion{
		Type:   "message",
		Status: string(responses.ResponseOutputMessageStatusCompleted),
		Content: []responses.ResponseOutputMessageContentUnion{
			{
				Type: "output_text",
				Text: text,
			},
		},
	}
}

func responseDiagnosticsRefusalItem(refusal string) responses.ResponseOutputItemUnion {
	return responses.ResponseOutputItemUnion{
		Type:   "message",
		Status: string(responses.ResponseOutputMessageStatusCompleted),
		Content: []responses.ResponseOutputMessageContentUnion{
			{
				Type:    "refusal",
				Refusal: refusal,
			},
		},
	}
}
