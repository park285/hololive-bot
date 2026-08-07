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

package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalPlaneTraceFilter(t *testing.T) {
	t.Parallel()

	for target, want := range map[string]bool{
		"/health":                          false,
		"/ready":                           false,
		"/internal/ready":                  false,
		"/metrics":                         false,
		"/api/videos":                      true,
		"/internal/trigger":                true,
		"/health/deep":                     true,
		"/__observability/trace-heartbeat": true,
	} {
		if got := LocalPlaneTraceFilter(httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)); got != want {
			t.Fatalf("LocalPlaneTraceFilter(%q) = %v, want %v", target, got, want)
		}
	}
}

func TestNewH2CServerAppliesTraceFilterOnlyWhenProvided(t *testing.T) {
	t.Parallel()

	if server := NewH2CServer(":0", http.NotFoundHandler(), "test.http"); server == nil {
		t.Fatal("NewH2CServer without a filter returned nil")
	}
	if server := NewH2CServer(":0", http.NotFoundHandler(), "test.http", LocalPlaneTraceFilter); server == nil {
		t.Fatal("NewH2CServer with a filter returned nil")
	}
	if got := firstTraceFilter(nil); got != nil {
		t.Fatal("firstTraceFilter(nil) must stay nil so the producer keeps healthcheck spans")
	}
	if got := firstTraceFilter([]func(*http.Request) bool{nil, LocalPlaneTraceFilter}); got == nil {
		t.Fatal("firstTraceFilter must skip nil entries")
	}
}
