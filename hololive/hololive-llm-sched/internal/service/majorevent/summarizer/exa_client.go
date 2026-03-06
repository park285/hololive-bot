package summarizer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/kapu/hololive-llm-sched/internal/model"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"

	"github.com/kapu/hololive-shared/pkg/constants"
)

// ExaMCPClient: Exa MCP JSON-RPC 2.0 검색 클라이언트
type ExaMCPClient struct {
	endpoint string
	apiKey   string
	client   *http.Client
	logger   *slog.Logger
}

// NewExaMCPClient: Exa MCP 클라이언트를 생성합니다.
func NewExaMCPClient(endpoint, apiKey string, httpClient *http.Client, logger *slog.Logger) *ExaMCPClient {
	if httpClient == nil {
		httpClient = httputil.DefaultClient()
	}

	return &ExaMCPClient{
		endpoint: endpoint,
		apiKey:   apiKey,
		client:   httpClient,
		logger:   logger,
	}
}

// Search: web_search_exa 도구를 호출해 검색 결과를 반환합니다.
func (c *ExaMCPClient) Search(ctx context.Context, query string) ([]model.SearchResult, error) {
	requestBody := map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"params": map[string]any{
			"name": "web_search_exa",
			"arguments": map[string]any{
				"query":      query,
				"numResults": 5,
				"type":       "auto",
				"livecrawl":  "auto",
			},
		},
		"id": 1,
	}

	bodyJSON, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal exa request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("create exa request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey := strings.TrimSpace(c.apiKey); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("X-Exa-Api-Key", apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exa request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if checkErr := httputil.CheckStatus(resp); checkErr != nil {
		return nil, fmt.Errorf("exa request: %w", checkErr)
	}

	respBody, err := jsonutil.ReadAllLimit(resp.Body, constants.APIConfig.MaxResponseBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read exa response: %w", err)
	}

	results, err := parseExaResponse(respBody)
	if err != nil {
		return nil, err
	}

	if c.logger != nil {
		c.logger.Debug("Exa MCP 검색 완료",
			slog.String("query", query),
			slog.Int("result_count", len(results)))
	}

	return results, nil
}

// parseExaResponse: JSON-RPC 응답을 파싱하여 검색 결과를 추출합니다.
func parseExaResponse(respBody []byte) ([]model.SearchResult, error) {
	var rpcResp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal exa response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("exa mcp rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	type exaItem struct {
		Title         string `json:"title"`
		URL           string `json:"url"`
		PublishedDate string `json:"publishedDate"`
		Text          string `json:"text"`
	}
	toResult := func(item exaItem) model.SearchResult {
		return model.SearchResult{
			Title:         item.Title,
			URL:           item.URL,
			Content:       item.Text,
			PublishedDate: item.PublishedDate,
		}
	}

	results := make([]model.SearchResult, 0)
	var parseErr error
	for i, content := range rpcResp.Result.Content {
		if content.Text == "" {
			continue
		}

		var wrapped struct {
			Results []exaItem `json:"results"`
		}
		if err := json.Unmarshal([]byte(content.Text), &wrapped); err != nil {
			var direct []exaItem
			if directErr := json.Unmarshal([]byte(content.Text), &direct); directErr != nil {
				parseErr = errors.Join(parseErr, fmt.Errorf("unmarshal exa content[%d]: %w", i, err))
				continue
			}
			for _, item := range direct {
				results = append(results, toResult(item))
			}
			continue
		}

		if len(wrapped.Results) == 0 {
			var direct []exaItem
			if err := json.Unmarshal([]byte(content.Text), &direct); err != nil {
				parseErr = errors.Join(parseErr, fmt.Errorf("unmarshal exa direct content[%d]: %w", i, err))
				continue
			}
			for _, item := range direct {
				results = append(results, toResult(item))
			}
			continue
		}

		for _, item := range wrapped.Results {
			results = append(results, toResult(item))
		}
	}

	if len(results) == 0 && parseErr != nil {
		return nil, parseErr
	}

	return results, nil
}
