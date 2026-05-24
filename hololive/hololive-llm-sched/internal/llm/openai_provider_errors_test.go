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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeProviderErrorError(t *testing.T) {
	t.Parallel()

	err := safeProviderError{
		statusCode: http.StatusBadRequest,
		code:       " invalid_request ",
		param:      " messages ",
		apiType:    " invalid_request_error ",
		errType:    " openai.Error ",
	}

	assert.Equal(t, "llm provider request failed status_code=400 code=invalid_request api_type=invalid_request_error param=messages error_type=openai.Error", err.Error())
}

func TestSafeLLMProviderError(t *testing.T) {
	t.Parallel()

	t.Run("nil returns nil", func(t *testing.T) {
		t.Parallel()

		assert.Nil(t, safeLLMProviderError(nil))
	})

	t.Run("empty output maps to empty output type", func(t *testing.T) {
		t.Parallel()

		err := safeLLMProviderError(fmt.Errorf("wrapped: %w", errOpenAIEmptyOutput))

		require.Error(t, err)
		assert.Equal(t, "llm provider request failed error_type=openai_empty_output", err.Error())
	})

	t.Run("openai error maps to safe error", func(t *testing.T) {
		t.Parallel()

		apiErr := newProviderErrorsOpenAIError()
		err := safeLLMProviderError(fmt.Errorf("wrapped: %w", apiErr))

		require.Error(t, err)
		assert.Equal(t, "llm provider request failed status_code=429 code=rate_limit api_type=rate_limit_error param=messages error_type=apierror.Error", err.Error())
	})
}

func TestLLMProviderErrorAttrs(t *testing.T) {
	t.Parallel()

	t.Run("nil returns nil", func(t *testing.T) {
		t.Parallel()

		assert.Nil(t, llmProviderErrorAttrs(nil))
	})

	t.Run("empty output returns type attr", func(t *testing.T) {
		t.Parallel()

		attrs := llmProviderErrorAttrs(fmt.Errorf("wrapped: %w", errOpenAIEmptyOutput))

		assert.Equal(t, map[string]any{
			"error_type": "openai_empty_output",
		}, slogAttrsByKey(attrs))
	})

	t.Run("openai error returns provider attrs", func(t *testing.T) {
		t.Parallel()

		attrs := llmProviderErrorAttrs(fmt.Errorf("wrapped: %w", newProviderErrorsOpenAIError()))

		assert.Equal(t, map[string]any{
			"error_type":           "apierror.Error",
			"provider_error":       true,
			"status_code":          http.StatusTooManyRequests,
			"error_code":           "rate_limit",
			"provider_error_type":  "rate_limit_error",
			"provider_error_param": "messages",
		}, slogAttrsByKey(attrs))
	})
}

func TestLLMErrorType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "nil returns empty",
			err:  nil,
			want: "",
		},
		{
			name: "typed error returns type name",
			err:  errors.New("plain"),
			want: "errors.errorString",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, llmErrorType(tt.err))
		})
	}
}

func TestOpenAIError(t *testing.T) {
	t.Parallel()

	t.Run("extracts wrapped openai error", func(t *testing.T) {
		t.Parallel()

		apiErr := newProviderErrorsOpenAIError()
		got, ok := openAIError(fmt.Errorf("wrapped: %w", apiErr))

		require.True(t, ok)
		assert.Same(t, apiErr, got)
	})

	t.Run("returns false for non-openai error", func(t *testing.T) {
		t.Parallel()

		got, ok := openAIError(errors.New("plain"))

		assert.False(t, ok)
		assert.Nil(t, got)
	})
}

func newProviderErrorsOpenAIError() *openai.Error {
	return &openai.Error{
		StatusCode: http.StatusTooManyRequests,
		Code:       " rate_limit ",
		Param:      " messages ",
		Type:       " rate_limit_error ",
	}
}

func slogAttrsByKey(attrs []slog.Attr) map[string]any {
	values := make(map[string]any, len(attrs))
	for _, attr := range attrs {
		switch attr.Value.Kind() {
		case slog.KindString:
			values[attr.Key] = attr.Value.String()
		case slog.KindBool:
			values[attr.Key] = attr.Value.Bool()
		case slog.KindInt64:
			values[attr.Key] = int(attr.Value.Int64())
		default:
			values[attr.Key] = attr.Value.Any()
		}
	}
	return values
}
