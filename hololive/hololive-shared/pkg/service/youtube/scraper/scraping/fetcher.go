package scraping

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/park285/shared-go/v2/pkg/httputil"
)

type FetcherEngine string

const (
	FetcherEngineNetHTTP         FetcherEngine = "nethttp"
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
	FinalURL   string
}

type netHTTPPageFetcher struct {
	client *Client
}

func normalizeFetcherEngine(engine FetcherEngine) FetcherEngine {
	switch engine {
	case FetcherEngineNetHTTP:
		return FetcherEngineNetHTTP
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
		return pageFetchResponse{}, fmt.Errorf("do: %w", err)
	}

	fetchResp := pageFetchResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		FinalURL:   finalResponseURL(resp),
	}

	if resp.StatusCode != http.StatusOK {
		if closeErr := closeUnsuccessfulFetchResponse(resp); closeErr != nil {
			return fetchResp, fmt.Errorf("close unsuccessful fetch response: %w", closeErr)
		}

		return fetchResp, nil
	}

	body, err := readSuccessfulFetchResponse(resp)
	if err != nil {
		return pageFetchResponse{}, fmt.Errorf("read successful fetch response: %w", err)
	}

	fetchResp.Body = body

	return fetchResp, nil
}

func (f netHTTPPageFetcher) do(req *http.Request) (*http.Response, error) {
	// 요청 URL은 전부 https://www.youtube.com/ 리터럴에 channelID나 videoID를 이어붙여 만든다.
	// 호스트가 이미 첫 슬래시 앞에서 확정되므로, 이어붙는 값이 무엇이든 다른 호스트로 향할 수 없다.
	//nolint:gosec // 위 주석대로 호스트가 리터럴로 고정되어 SSRF가 성립하지 않는다.
	resp, err := f.client.currentHTTPClient().Do(req)
	if err != nil {
		if resp == nil {
			err = fmt.Errorf("nil response: %w", err)
		}

		return nil, fmt.Errorf("failed to fetch page: %w", err)
	}

	if resp == nil {
		return nil, errors.New("failed to fetch page: nil response")
	}

	if resp.Body == nil {
		return nil, errors.New("failed to fetch page: nil response body")
	}

	return resp, nil
}

func finalResponseURL(resp *http.Response) string {
	if resp == nil || resp.Request == nil || resp.Request.URL == nil {
		return ""
	}

	return resp.Request.URL.String()
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
	body, err := httputil.ReadAllLimited(resp.Body, ytDefaults.MaxPageBodyBytes)
	closeErr := resp.Body.Close()

	if err != nil {
		if closeErr != nil {
			return nil, fmt.Errorf("failed to read response body: %w", errors.Join(err, fmt.Errorf("close response body: %w", closeErr)))
		}

		if readErr := responseBodyReadError(err); readErr != nil {
			return nil, fmt.Errorf("response body read error: %w", readErr)
		}

		return nil, nil
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

	if errors.Is(err, httputil.ErrResponseBodyTooLarge) {
		return fmt.Errorf("%w: %w", ErrResponseTooLarge, err)
	}

	return fmt.Errorf("failed to read response body: %w", err)
}
