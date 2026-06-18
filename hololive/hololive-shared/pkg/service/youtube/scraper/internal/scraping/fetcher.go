package scraping

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/park285/shared-go/pkg/jsonutil"
)

type FetcherEngine string

const (
	FetcherEngineNetHTTP         FetcherEngine = "nethttp"
	FetcherEngineGoScrapy        FetcherEngine = "goscrapy"
	FetcherEngineBrowserSnapshot FetcherEngine = "browser_snapshot"
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
	switch engine {
	case FetcherEngineNetHTTP:
		return FetcherEngineNetHTTP
	case FetcherEngineGoScrapy:
		return FetcherEngineGoScrapy
	case FetcherEngineBrowserSnapshot:
		return FetcherEngineBrowserSnapshot
	default:
		return FetcherEngineNetHTTP
	}
}

func (f netHTTPPageFetcher) FetchPage(ctx context.Context, fetchReq pageFetchRequest) (pageFetchResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchReq.URL, http.NoBody)
	if err != nil {
		return pageFetchResponse{}, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header = fetchReq.Header.Clone()

	resp, err := f.do(req)
	if err != nil {
		return pageFetchResponse{}, err
	}
	fetchResp := pageFetchResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
	}

	if resp.StatusCode != http.StatusOK {
		return fetchResp, closeUnsuccessfulFetchResponse(resp)
	}

	body, err := readSuccessfulFetchResponse(resp)
	if err != nil {
		return pageFetchResponse{}, err
	}
	fetchResp.Body = body

	return fetchResp, nil
}

func (f netHTTPPageFetcher) do(req *http.Request) (*http.Response, error) {
	//nolint:gosec // G704: internal fetcher accepts caller-built URLs; production callers construct YouTube URLs and tests use httptest URLs.
	resp, err := f.client.currentHTTPClient().Do(req)
	if err != nil {
		if resp == nil {
			err = fmt.Errorf("nil response: %w", err)
		}
		return nil, fmt.Errorf("failed to fetch page: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("failed to fetch page: nil response")
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("failed to fetch page: nil response body")
	}
	return resp, nil
}

func closeUnsuccessfulFetchResponse(resp *http.Response) error {
	if err := drainResponseBody(resp); err != nil {
		return fmt.Errorf("drain response body: %w", err)
	}
	if err := resp.Body.Close(); err != nil {
		return fmt.Errorf("close response body: %w", err)
	}
	return nil
}

func readSuccessfulFetchResponse(resp *http.Response) ([]byte, error) {
	body, err := jsonutil.ReadAllLimit(resp.Body, ytDefaults.MaxPageBodyBytes)
	closeErr := resp.Body.Close()
	if err != nil {
		if closeErr != nil {
			return nil, fmt.Errorf("failed to read response body: %w", errors.Join(err, fmt.Errorf("close response body: %w", closeErr)))
		}
		return nil, responseBodyReadError(err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close response body: %w", closeErr)
	}
	return body, nil
}

func responseBodyReadError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	if errors.Is(err, jsonutil.ErrBodyTooLarge) {
		return fmt.Errorf("%w: %w", ErrResponseTooLarge, err)
	}
	return fmt.Errorf("failed to read response body: %w", err)
}
