package runtime

import (
	jsonv2 "encoding/json/v2"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/park285/shared-go/v2/pkg/llm/openaipreset"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/llm"
)

const (
	testFeatureDefaultModel = "cliproxy-default-model"
	testReasoningEffort     = "high"
	responsesPath           = "/responses"
	chatCompletionsPath     = "/chat/completions"
)

type capturedProviderRequest struct {
	mu            sync.Mutex
	path          string
	authorization string
	body          map[string]any
}

func (c *capturedProviderRequest) snapshot() (string, string, map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.path, c.authorization, c.body
}

// newRejectingProviderServer는 요청을 기록하고 400으로 거절한다. 400은 SDK 재시도 대상이 아니다.
func newRejectingProviderServer(t *testing.T) (*httptest.Server, *capturedProviderRequest) {
	t.Helper()

	captured := &capturedProviderRequest{}
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
		captured.authorization = r.Header.Get("Authorization")
		captured.body = body
		captured.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)

		if _, err := io.WriteString(w, `{"error":{"message":"rejected by test","type":"invalid_request_error"}}`); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	return server, captured
}

type wantProviderRequest struct {
	path        string
	model       string
	temperature float64
	webSearch   bool
	schemaName  string
}

// 기능별 cliproxy 요청 형태(경로·인증 헤더·모델·temperature·reasoning·web search·schema 이름)는 provider 계약이다.
// 요약 기능 가운데 major event만 Responses web search를 쓰고, member news 계열은 Chat Completions로 간다.
func TestCliproxyProviderClientsSendFeatureRequestShape(t *testing.T) {
	llmConfig := &apiconfig.LLMConfig{
		MemberNewsModel:       "member-model",
		MemberNewsTemperature: 0.3,
		MemberNews:            apiconfig.ConsensusLLMConfig{Enabled: true, ReviewerModel: "member-reviewer-model", AdjudicatorModel: "member-adjudicator-model"},
		MajorEvent:            apiconfig.ConsensusLLMConfig{Enabled: true, ReviewerModel: "event-reviewer-model", AdjudicatorModel: "event-adjudicator-model"},
	}

	for _, tt := range []struct {
		feature string
		want    wantProviderRequest
	}{
		{feature: "major event summary", want: wantProviderRequest{path: responsesPath, model: testFeatureDefaultModel, webSearch: true, schemaName: "event_summary"}},
		{feature: "major event reviewer", want: wantProviderRequest{path: responsesPath, model: "event-reviewer-model", schemaName: "event_summary_review"}},
		{feature: "major event adjudicator", want: wantProviderRequest{path: responsesPath, model: "event-adjudicator-model", schemaName: "event_summary"}},
		{feature: "member news summary", want: wantProviderRequest{path: chatCompletionsPath, model: "member-model", temperature: 0.3}},
		{feature: "member news reviewer", want: wantProviderRequest{path: chatCompletionsPath, model: "member-reviewer-model", temperature: 0.1}},
		{feature: "member news adjudicator", want: wantProviderRequest{path: chatCompletionsPath, model: "member-adjudicator-model", temperature: 0.3}},
	} {
		t.Run(tt.feature, func(t *testing.T) {
			server, captured := newRejectingProviderServer(t)
			provider := cliproxyProvider(apiconfig.CliproxyConfig{
				Enabled:         true,
				APIKey:          testProviderKey,
				BaseURL:         server.URL,
				Model:           testFeatureDefaultModel,
				ReasoningEffort: testReasoningEffort,
			})
			client := buildFeatureClient(t, tt.feature, provider, llmConfig)

			prompts := openaipreset.PromptLayers{Developer: "developer rules", User: "user input"}
			if _, err := client.GenerateJSON(t.Context(), prompts, map[string]any{"type": "object"}); err == nil {
				t.Fatal("GenerateJSON() error = nil, want test server rejection")
			}

			path, authorization, body := captured.snapshot()
			if path != tt.want.path {
				t.Fatalf("path = %q, want %q", path, tt.want.path)
			}

			if authorization != "Bearer "+testProviderKey {
				t.Fatalf("Authorization = %q, want bearer API key", authorization)
			}

			assertCommonRequestBody(t, body, tt.want)

			if tt.want.path == chatCompletionsPath {
				assertChatCompletionsRequestBody(t, body)

				return
			}

			assertResponsesRequestBody(t, body, tt.want)
		})
	}
}

func buildFeatureClient(t *testing.T, feature string, provider apiconfig.LLMProviderConfig, llmConfig *apiconfig.LLMConfig) llm.Client {
	t.Helper()

	logger := slog.New(slog.DiscardHandler)

	var (
		client llm.Client
		err    error
	)

	switch feature {
	case "major event summary":
		client = ProvideMajorEventLLMClient(provider, nil, logger)
	case "major event reviewer":
		client = ProvideMajorEventReviewerClient(provider, llmConfig, nil, logger)
	case "major event adjudicator":
		client = ProvideMajorEventAdjudicatorClient(provider, llmConfig, nil, logger)
	case "member news summary":
		client, err = ProvideMemberNewsLLMClient(provider, llmConfig, nil, logger)
	case "member news reviewer":
		client, err = ProvideMemberNewsReviewerClient(provider, llmConfig, nil, logger)
	case "member news adjudicator":
		client, err = ProvideMemberNewsAdjudicatorClient(provider, llmConfig, nil, logger)
	default:
		t.Fatalf("unknown feature %q", feature)
	}

	if err != nil || client == nil {
		t.Fatalf("build %s client = (%v, %v), want client", feature, client, err)
	}

	return client
}

func assertCommonRequestBody(t *testing.T, body map[string]any, want wantProviderRequest) {
	t.Helper()

	if body["model"] != want.model {
		t.Fatalf("model = %v, want %q", body["model"], want.model)
	}

	temperature, hasTemperature := body["temperature"]
	if want.temperature == 0 && hasTemperature {
		t.Fatalf("temperature = %v, want omitted", temperature)
	}

	if want.temperature != 0 && temperature != want.temperature {
		t.Fatalf("temperature = %v, want %v", temperature, want.temperature)
	}
}

func assertChatCompletionsRequestBody(t *testing.T, body map[string]any) {
	t.Helper()

	if body["reasoning_effort"] != testReasoningEffort {
		t.Fatalf("reasoning_effort = %v, want %q", body["reasoning_effort"], testReasoningEffort)
	}

	for _, key := range []string{"tools", "response_format"} {
		if value, ok := body[key]; ok {
			t.Fatalf("chat completions request carries %s=%v", key, value)
		}
	}
}

func assertResponsesRequestBody(t *testing.T, body map[string]any, want wantProviderRequest) {
	t.Helper()

	if effort := requestObject(t, body["reasoning"], "reasoning")["effort"]; effort != testReasoningEffort {
		t.Fatalf("reasoning.effort = %v, want %q", effort, testReasoningEffort)
	}

	format := requestObject(t, requestObject(t, body["text"], "text")["format"], "text.format")
	if format["name"] != want.schemaName {
		t.Fatalf("text.format.name = %v, want %q", format["name"], want.schemaName)
	}

	tools, hasTools := body["tools"]

	if !want.webSearch {
		if hasTools {
			t.Fatalf("tools = %v, want omitted without web search", tools)
		}

		return
	}

	toolList, ok := tools.([]any)
	if !ok || len(toolList) != 1 {
		t.Fatalf("tools = %v, want single web search tool", tools)
	}

	if toolType := requestObject(t, toolList[0], "tools[0]")["type"]; toolType != "web_search" {
		t.Fatalf("tools[0].type = %v, want web_search", toolType)
	}
}

func requestObject(t *testing.T, value any, name string) map[string]any {
	t.Helper()

	object, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("request %s = %#v, want object", name, value)
	}

	return object
}

// 이름 정규화는 config 적재 단계 한 곳에서 한다. 따라서 runtime은 빈 이름을 cliproxy로 바꾸지 않고
// 기능 비활성이 아닌 초기화 오류로 돌려준다.
func TestProvideMemberNewsLLMClientRejectsEmptyProviderName(t *testing.T) {
	client, err := ProvideMemberNewsLLMClient(apiconfig.LLMProviderConfig{
		Cliproxy: apiconfig.CliproxyConfig{Enabled: true, APIKey: testProviderKey, BaseURL: testProviderBaseURL, Model: "gpt-test"},
	}, &apiconfig.LLMConfig{}, nil, slog.New(slog.DiscardHandler))
	if client != nil || err == nil || isLLMFeatureDisabled(err) {
		t.Fatalf("got (%v, %v), want unsupported provider initialization error", client, err)
	}
}
