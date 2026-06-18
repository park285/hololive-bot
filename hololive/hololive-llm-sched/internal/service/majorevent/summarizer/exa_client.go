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
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/httputil"
	json "github.com/park285/shared-go/pkg/json"
	"github.com/park285/shared-go/pkg/jsonutil"

	"github.com/kapu/hololive-llm-sched/internal/model"

	"github.com/kapu/hololive-shared/pkg/config"
)

type ExaMCPClient struct {
	endpoint string
	apiKey   string
	client   *http.Client
	logger   *slog.Logger
}

type exaRPCResponse struct {
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

type exaSearchItem struct {
	Title         string `json:"title"`
	URL           string `json:"url"`
	PublishedDate string `json:"publishedDate"`
	Text          string `json:"text"`
}

func NewExaMCPClient(endpoint, apiKey string, httpClient *http.Client, logger *slog.Logger) *ExaMCPClient {
	if httpClient == nil {
		httpClient = httputil.NewExternalAPIClient(30 * time.Second)
	}

	return &ExaMCPClient{
		endpoint: endpoint,
		apiKey:   apiKey,
		client:   httpClient,
		logger:   logger,
	}
}

func (c *ExaMCPClient) Search(ctx context.Context, query string) ([]model.SearchResult, error) {
	req, err := c.newSearchRequest(ctx, query)
	if err != nil {
		return nil, err
	}

	respBody, err := c.doSearchRequest(req)
	if err != nil {
		return nil, err
	}

	results, err := parseExaResponse(respBody)
	if err != nil {
		return nil, err
	}

	c.logSearchResults(query, len(results))

	return results, nil
}

func (c *ExaMCPClient) newSearchRequest(ctx context.Context, query string) (*http.Request, error) {
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

	return req, nil
}

func (c *ExaMCPClient) doSearchRequest(req *http.Request) ([]byte, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exa request: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("exa request: response is nil")
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("exa request: response body is nil")
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil && c.logger != nil {
			c.logger.Debug("close exa response body failed", slog.Any("error", closeErr))
		}
	}()

	if checkErr := httputil.CheckStatus(resp); checkErr != nil {
		return nil, fmt.Errorf("exa request: %w", checkErr)
	}

	respBody, err := jsonutil.ReadAllLimit(resp.Body, config.DefaultMaxResponseBodyBytes)
	if err != nil {
		return nil, fmt.Errorf("read exa response: %w", err)
	}

	return respBody, nil
}

func (c *ExaMCPClient) logSearchResults(query string, resultCount int) {
	if c.logger != nil {
		c.logger.Debug("Exa MCP 검색 완료",
			slog.String("query", query),
			slog.Int("result_count", resultCount))
	}
}

// parseExaResponse: JSON-RPC 응답을 파싱하여 검색 결과를 추출합니다.
func parseExaResponse(respBody []byte) ([]model.SearchResult, error) {
	var rpcResp exaRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal exa response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("exa mcp rpc error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	results := make([]model.SearchResult, 0)
	var parseErr error
	for i, content := range rpcResp.Result.Content {
		items, err := parseExaContentItems(i, content.Text)
		if err != nil {
			parseErr = errors.Join(parseErr, err)
			continue
		}
		results = append(results, exaItemsToSearchResults(items)...)
	}

	if len(results) == 0 && parseErr != nil {
		return nil, parseErr
	}

	return results, nil
}

func parseExaContentItems(index int, text string) ([]exaSearchItem, error) {
	if text == "" {
		return nil, nil
	}

	var wrapped struct {
		Results []exaSearchItem `json:"results"`
	}
	if err := json.Unmarshal([]byte(text), &wrapped); err != nil {
		direct, directErr := parseDirectExaContentItems(text)
		if directErr != nil {
			return nil, fmt.Errorf("unmarshal exa content[%d]: %w", index, err)
		}
		return direct, nil
	}

	if len(wrapped.Results) == 0 {
		direct, err := parseDirectExaContentItems(text)
		if err != nil {
			return nil, fmt.Errorf("unmarshal exa direct content[%d]: %w", index, err)
		}
		return direct, nil
	}

	return wrapped.Results, nil
}

func parseDirectExaContentItems(text string) ([]exaSearchItem, error) {
	var direct []exaSearchItem
	if err := json.Unmarshal([]byte(text), &direct); err != nil {
		return nil, err
	}
	return direct, nil
}

func exaItemsToSearchResults(items []exaSearchItem) []model.SearchResult {
	results := make([]model.SearchResult, 0, len(items))
	for _, item := range items {
		results = append(results, model.SearchResult{
			Title:         item.Title,
			URL:           item.URL,
			Content:       item.Text,
			PublishedDate: item.PublishedDate,
		})
	}
	return results
}
