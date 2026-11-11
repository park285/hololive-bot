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
	"log/slog"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeProviderErrorError(t *testing.T) {
	t.Parallel()

	err := safeProviderError{
		statusCode: http.StatusBadRequest,
		code:       " invalid_request ",
		apiType:    " gemini ",
		errType:    " interaction_not_completed ",
	}

	assert.Equal(t, "llm provider request failed status_code=400 code=invalid_request api_type=gemini error_type=interaction_not_completed", err.Error())
}

func TestSafeLLMProviderError(t *testing.T) {
	t.Parallel()

	t.Run("nil returns nil", func(t *testing.T) {
		t.Parallel()

		assert.NoError(t, safeLLMProviderError(nil))
	})

	// 분류는 오류 타입으로만 한다. 문구가 안전한 진단처럼 보여도 타입이 없으면 원문을 옮기지 않는다.
	t.Run("untyped error with safe-looking prefix is classified by type", func(t *testing.T) {
		t.Parallel()

		err := safeLLMProviderError(errors.New("llm provider request failed upstream_detail=raw"))

		require.Error(t, err)
		assert.Equal(t, "llm provider request failed error_type=errors.errorString", err.Error())
	})
}

func TestLLMProviderErrorAttrs(t *testing.T) {
	t.Parallel()

	t.Run("nil returns nil", func(t *testing.T) {
		t.Parallel()

		assert.Nil(t, llmProviderErrorAttrs(nil))
	})

	t.Run("safe provider error returns provider attrs", func(t *testing.T) {
		t.Parallel()

		attrs := llmProviderErrorAttrs(safeProviderError{
			statusCode: http.StatusTooManyRequests,
			code:       " RESOURCE_EXHAUSTED ",
			apiType:    "gemini",
			errType:    "interaction_not_completed",
		})

		assert.Equal(t, map[string]any{
			"error_type":          "interaction_not_completed",
			"status_code":         http.StatusTooManyRequests,
			"error_code":          "RESOURCE_EXHAUSTED",
			"provider_error_type": "gemini",
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
		case slog.KindAny, slog.KindDuration, slog.KindFloat64, slog.KindTime, slog.KindUint64, slog.KindGroup, slog.KindLogValuer:
			values[attr.Key] = attr.Value.Any()
		default:
			values[attr.Key] = attr.Value.Any()
		}
	}

	return values
}
