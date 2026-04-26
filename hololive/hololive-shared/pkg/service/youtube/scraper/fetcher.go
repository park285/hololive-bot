package scraper

import (
	"context"
	"fmt"
	"net/http"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/jsonutil"

	"github.com/kapu/hololive-shared/pkg/constants"
)

type FetcherEngine string

const (
	FetcherEngineNetHTTP  FetcherEngine = "nethttp"
	FetcherEngineGoScrapy FetcherEngine = "goscrapy"
)

type pageFetcher interface {
	FetchPage(ctx context.Context, req pageFetchRequest) (pageFetchResponse, error)
}

type pageFetchRequest struct {
	URL    string
	Header http.Header
}

type pageFetchResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

type netHTTPPageFetcher struct {
	client *Client
}

func normalizeFetcherEngine(engine FetcherEngine) FetcherEngine {
	if engine == FetcherEngineGoScrapy {
		return FetcherEngineGoScrapy
	}
	return FetcherEngineNetHTTP
}

func (f netHTTPPageFetcher) FetchPage(ctx context.Context, fetchReq pageFetchRequest) (pageFetchResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchReq.URL, http.NoBody)
	if err != nil {
		return pageFetchResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header = fetchReq.Header.Clone()

	resp, err := f.client.currentHTTPClient().Do(req)
	if err != nil {
		return pageFetchResponse{}, fmt.Errorf("failed to fetch page: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	fetchResp := pageFetchResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
	}

	if resp.StatusCode != http.StatusOK {
		drainResponseBody(resp)
		return fetchResp, nil
	}

	body, err := jsonutil.ReadAllLimit(resp.Body, constants.YouTubeConfig.MaxPageBodyBytes)
	if err != nil {
		return pageFetchResponse{}, fmt.Errorf("failed to read response body: %w", err)
	}
	fetchResp.Body = body

	return fetchResp, nil
}
