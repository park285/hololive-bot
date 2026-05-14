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
	"strings"
	"syscall"

	"github.com/openai/openai-go/v3"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

func shouldFallbackToChatCompletions(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errOpenAIEmptyOutput) {
		return true
	}

	if shouldFallbackOpenAIError(err) {
		return true
	}

	return shouldFallbackNetworkError(err)
}

func shouldFallbackOpenAIError(err error) bool {
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		if shouldFallbackOpenAIStatus(apiErr.StatusCode) {
			return true
		}

		return shouldFallbackOpenAICode(apiErr.Code)
	}

	return false
}

func shouldFallbackOpenAICode(code string) bool {
	switch strings.ToLower(code) {
	case "unsupported", "unsupported_endpoint", "unsupported_api", "not_implemented":
		return true
	default:
		return false
	}
}

func shouldFallbackNetworkError(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	return false
}

func shouldFallbackOpenAIStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func suppressFallbackDiscoveredEvents(rawJSON string) (string, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &payload); err != nil {
		return "", fmt.Errorf("parse fallback json: %w", err)
	}

	if _, exists := payload["discovered_events"]; !exists {
		return rawJSON, nil
	}

	payload["discovered_events"] = make([]any, 0)

	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal sanitized json: %w", err)
	}
	return string(b), nil
}
