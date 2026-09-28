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
	jsonv2 "encoding/json/v2"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/park285/shared-go/v2/pkg/llm/openaipreset"
)

// DEC-20260926-stack-llm-instruction-layering-sole-path: hololive client는 지시를 계층으로만 보내며,
// provider 요청 본문에는 단일 system prompt 경로(Responses `instructions`)가 나타나지 않는다.

const testWireUserInput = "user input"

func testWirePromptLayers() openaipreset.PromptLayers {
	return openaipreset.PromptLayers{Invariant: "invariant rules", Developer: "developer rules", User: testWireUserInput}
}

type capturedLLMRequest struct {
	mu   sync.Mutex
	path string
	body map[string]any
}

func (c *capturedLLMRequest) snapshot() (string, map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.path, c.body
}

// newCapturingLLMServer는 첫 요청 본문을 기록하고 status/response로 응답한다. 400은 SDK가 재시도하지 않는다.
func newCapturingLLMServer(t *testing.T, status int, response string) (*httptest.Server, *capturedLLMRequest) {
	t.Helper()

	captured := &capturedLLMRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}

		var body map[string]any

		if err := jsonv2.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode request body: %v", err)
		}

		captured.mu.Lock()

		captured.path = r.URL.Path
		captured.body = body
		captured.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)

		if _, err := io.WriteString(w, response); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	return server, captured
}

func requestMessages(t *testing.T, body map[string]any, key string) []map[string]any {
	t.Helper()

	items, ok := body[key].([]any)
	if !ok {
		t.Fatalf("request %s = %#v, want message list", key, body[key])
	}

	messages := make([]map[string]any, 0, len(items))
	for _, item := range items {
		message, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("request %s item = %#v, want object", key, item)
		}

		messages = append(messages, message)
	}

	return messages
}

const badRequestBody = `{"error":{"message":"rejected by test","type":"invalid_request_error"}}`

func TestOpenAIClientResponsesSendsInstructionLayersAsDeveloperMessages(t *testing.T) {
	server, captured := newCapturingLLMServer(t, http.StatusBadRequest, badRequestBody)
	client := mustNewClient(t, server.URL, "test-key", "gpt-test", slog.New(slog.DiscardHandler), WithWebSearch(false))

	if _, err := client.GenerateJSON(t.Context(), testWirePromptLayers(), testObjectSchema()); err == nil {
		t.Fatal("GenerateJSON() error = nil, want test server rejection")
	}

	path, body := captured.snapshot()
	if path != "/responses" {
		t.Fatalf("path = %q, want /responses", path)
	}

	if _, ok := body["instructions"]; ok {
		t.Fatalf("request carries single-instruction field instructions=%#v", body["instructions"])
	}

	messages := requestMessages(t, body, "input")
	want := []struct{ role, content string }{
		{"developer", "[APPLICATION INVARIANTS]\ninvariant rules"},
		{"developer", "[DEVELOPER INSTRUCTIONS]\ndeveloper rules"},
		{"user", testWireUserInput},
	}

	if len(messages) != len(want) {
		t.Fatalf("input messages = %#v, want %d", messages, len(want))
	}

	for i, expected := range want {
		if messages[i]["role"] != expected.role || messages[i]["content"] != expected.content {
			t.Fatalf("input[%d] = role:%v content:%v, want role:%s content:%q", i, messages[i]["role"], messages[i]["content"], expected.role, expected.content)
		}
	}
}

func TestOpenAIClientChatCompletionsSendsInstructionLayersInSingleSystemMessage(t *testing.T) {
	server, captured := newCapturingLLMServer(t, http.StatusBadRequest, badRequestBody)
	client := mustNewClient(t, server.URL, "test-key", "gpt-test", slog.New(slog.DiscardHandler), WithChatCompletions())

	if _, err := client.GenerateJSON(t.Context(), testWirePromptLayers(), testObjectSchema()); err == nil {
		t.Fatal("GenerateJSON() error = nil, want test server rejection")
	}

	path, body := captured.snapshot()
	if path != "/chat/completions" {
		t.Fatalf("path = %q, want /chat/completions", path)
	}

	assertSingleSystemLayeredMessages(t, requestMessages(t, body, "messages"))
}

func assertSingleSystemLayeredMessages(t *testing.T, messages []map[string]any) {
	t.Helper()

	if len(messages) != 2 {
		t.Fatalf("messages = %#v, want system and user", messages)
	}

	system, ok := messages[0]["content"].(string)
	if !ok || messages[0]["role"] != "system" || !strings.HasPrefix(system, "[APPLICATION INVARIANTS]\ninvariant rules\n\n[DEVELOPER INSTRUCTIONS]\ndeveloper rules") {
		t.Fatalf("messages[0] = role:%v content:%q, want labeled single system message", messages[0]["role"], system)
	}

	if messages[1]["role"] != "user" || messages[1]["content"] != testWireUserInput {
		t.Fatalf("messages[1] = %#v, want user input", messages[1])
	}
}

func TestPresetClientSendsInstructionLayersAndReturnsRawJSON(t *testing.T) {
	const completion = `{"id":"chatcmpl-test","object":"chat.completion","created":0,"model":"gpt-test",` +
		`"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"ok\":true}"}}]}`

	server, captured := newCapturingLLMServer(t, http.StatusOK, completion)

	client, err := NewPresetClient(server.URL, "test-key", "gpt-test", slog.New(slog.DiscardHandler),
		WithSchemaName("member_news_summary"), WithChatCompletions(), WithWebSearch(false))
	if err != nil {
		t.Fatalf("NewPresetClient() error = %v", err)
	}

	got, err := client.GenerateJSON(t.Context(), testWirePromptLayers(), testObjectSchema())
	if err != nil {
		t.Fatalf("GenerateJSON() error = %v", err)
	}

	if got != `{"ok":true}` {
		t.Fatalf("GenerateJSON() = %q, want provider JSON", got)
	}

	path, body := captured.snapshot()
	if path != "/chat/completions" {
		t.Fatalf("path = %q, want /chat/completions", path)
	}

	assertSingleSystemLayeredMessages(t, requestMessages(t, body, "messages"))
}

func TestNewPresetClientRejectsEmptySchemaName(t *testing.T) {
	if _, err := NewPresetClient("https://example.com/v1", "test-key", "gpt-test", slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("NewPresetClient() error = nil, want empty schema name rejection")
	}
}
