package majorevent

import (
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"testing"
)

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
