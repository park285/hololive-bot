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
	"net"
	"net/http"
	"syscall"
	"testing"

	json "github.com/park285/hololive-bot/shared-go/pkg/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShouldFallbackToChatCompletions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error returns false",
			err:  nil,
			want: false,
		},
		{
			name: "empty output returns true",
			err:  fmt.Errorf("openai responses API: %w", errOpenAIEmptyOutput),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, shouldFallbackToChatCompletions(tt.err))
		})
	}
}

func TestShouldFallbackOpenAIStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		statusCode int
		want       bool
	}{
		{statusCode: http.StatusNotFound, want: true},
		{statusCode: http.StatusMethodNotAllowed, want: true},
		{statusCode: http.StatusInternalServerError, want: true},
		{statusCode: http.StatusBadGateway, want: true},
		{statusCode: http.StatusServiceUnavailable, want: true},
		{statusCode: http.StatusGatewayTimeout, want: true},
		{statusCode: http.StatusBadRequest, want: false},
		{statusCode: http.StatusUnauthorized, want: false},
		{statusCode: http.StatusForbidden, want: false},
		{statusCode: http.StatusTooManyRequests, want: false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.statusCode), func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, shouldFallbackOpenAIStatus(tt.statusCode))
		})
	}
}

func TestShouldFallbackOpenAICode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code string
		want bool
	}{
		{name: "unsupported", code: "unsupported", want: true},
		{name: "not implemented", code: "not_implemented", want: true},
		{name: "invalid request", code: "invalid_request_error", want: false},
		{name: "rate limit", code: "rate_limit_exceeded", want: false},
		{name: "empty", code: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, shouldFallbackOpenAICode(tt.code))
		})
	}
}

func TestShouldFallbackNetworkError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "connection refused returns true",
			err:  fmt.Errorf("dial tcp: %w", syscall.ECONNREFUSED),
			want: true,
		},
		{
			name: "timeout returns true",
			err:  &net.DNSError{IsTimeout: true},
			want: true,
		},
		{
			name: "plain error returns false",
			err:  errors.New("provider failed"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, shouldFallbackNetworkError(tt.err))
		})
	}
}

func TestSuppressFallbackDiscoveredEvents(t *testing.T) {
	t.Parallel()

	t.Run("clears discovered events", func(t *testing.T) {
		t.Parallel()

		got, err := suppressFallbackDiscoveredEvents(`{"summary":"ok","discovered_events":[{"id":"a"}]}`)
		require.NoError(t, err)

		var payload map[string]any
		require.NoError(t, json.Unmarshal([]byte(got), &payload))
		assert.Equal(t, "ok", payload["summary"])
		assert.Empty(t, payload["discovered_events"])
	})

	t.Run("no-op when key absent", func(t *testing.T) {
		t.Parallel()

		raw := `{"summary":"ok"}`
		got, err := suppressFallbackDiscoveredEvents(raw)

		require.NoError(t, err)
		assert.Equal(t, raw, got)
	})

	t.Run("invalid json returns error", func(t *testing.T) {
		t.Parallel()

		_, err := suppressFallbackDiscoveredEvents(`{"summary":`)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse fallback json")
	})
}
