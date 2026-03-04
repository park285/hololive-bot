package summarizer

import (
	"context"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
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
		_, _ = w.Write(buildExaRPCBody(t, []string{
			mustJSONString(t, []map[string]any{
				{
					"title":         "sample",
					"url":           "https://example.com",
					"text":          "sample text",
					"publishedDate": "2026-03-04",
				},
			}),
		}))
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
