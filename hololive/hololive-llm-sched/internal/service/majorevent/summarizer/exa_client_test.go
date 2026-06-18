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

package summarizer

import (
	"context"
	json "github.com/park285/shared-go/pkg/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExaMCPClientSearch_SendsAPIKeyViaHeaderNotQuery(t *testing.T) {
	var (
		gotAuthorization string
		gotHeaderKey     string
		gotRawQuery      string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		gotHeaderKey = r.Header.Get("X-Exa-Api-Key")
		gotRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(buildExaRPCBody(t, []string{
			mustJSONString(t, []map[string]any{
				{
					"title":         "sample",
					"url":           "https://example.com",
					"text":          "sample text",
					"publishedDate": "2026-03-04",
				},
			}),
		})); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	client := NewExaMCPClient(server.URL+"?existing=1", "secret-key", server.Client(), nil)
	results, err := client.Search(context.Background(), "hololive")
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if gotAuthorization != "Bearer secret-key" {
		t.Fatalf("Authorization = %q, want %q", gotAuthorization, "Bearer secret-key")
	}
	if gotHeaderKey != "secret-key" {
		t.Fatalf("X-Exa-Api-Key = %q, want %q", gotHeaderKey, "secret-key")
	}
	if strings.Contains(gotRawQuery, "exaApiKey=") {
		t.Fatalf("query should not include exaApiKey, got: %q", gotRawQuery)
	}
}

func TestParseExaResponse_WrappedResults(t *testing.T) {
	body := buildExaRPCBody(t, []string{
		mustJSONString(t, map[string]any{
			"results": []map[string]any{
				{
					"title":         "Hoshimachi Suisei Live",
					"url":           "https://example.com/suisei",
					"text":          "live info",
					"publishedDate": "2026-02-18",
				},
			},
		}),
	})

	results, err := parseExaResponse(body)
	if err != nil {
		t.Fatalf("parseExaResponse() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].Title != "Hoshimachi Suisei Live" {
		t.Errorf("title = %q", results[0].Title)
	}
}

func TestParseExaResponse_PartialMalformedContent(t *testing.T) {
	body := buildExaRPCBody(t, []string{
		"{malformed-json",
		mustJSONString(t, []map[string]any{
			{
				"title":         "ANIPLUS Cafe",
				"url":           "https://x.com/ANIPLUS_SHOP/status/1",
				"text":          "collab cafe",
				"publishedDate": "2026-02-18",
			},
		}),
	})

	results, err := parseExaResponse(body)
	if err != nil {
		t.Fatalf("parseExaResponse() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].URL != "https://x.com/ANIPLUS_SHOP/status/1" {
		t.Errorf("url = %q", results[0].URL)
	}
}

func TestParseExaResponse_AllMalformed(t *testing.T) {
	body := buildExaRPCBody(t, []string{
		"{bad}",
		"{bad2}",
	})

	_, err := parseExaResponse(body)
	if err == nil {
		t.Fatal("parseExaResponse() expected error, got nil")
	}
}

func buildExaRPCBody(t *testing.T, texts []string) []byte {
	t.Helper()
	content := make([]map[string]string, 0, len(texts))
	for _, text := range texts {
		content = append(content, map[string]string{"text": text})
	}

	raw, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"result": map[string]any{
			"content": content,
		},
		"id": 1,
	})
	if err != nil {
		t.Fatalf("marshal rpc body: %v", err)
	}
	return raw
}

func mustJSONString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json string: %v", err)
	}
	return string(b)
}
