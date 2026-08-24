package scraping

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/internal/browserfetcher"
)

type BrowserSnapshotFetcher struct {
	inner *browserfetcher.Fetcher
}

func NewBrowserSnapshotFetcher(endpoint string, timeout time.Duration) *BrowserSnapshotFetcher {
	return &BrowserSnapshotFetcher{inner: browserfetcher.New(endpoint, timeout)}
}

//nolint:revive // unexported-return: 패키지 내부 pageFetcher 인터페이스 구현이라 반환 타입을 내보낼 수 없다.
func (f *BrowserSnapshotFetcher) FetchPage(ctx context.Context, req pageFetchRequest) (pageFetchResponse, error) {
	if f == nil || f.inner == nil {
		return pageFetchResponse{}, errors.New("browser snapshot endpoint is not configured")
	}

	resp, err := f.inner.FetchPage(ctx, browserfetcher.Request{
		URL:    req.URL,
		Header: req.Header,
	})
	if err != nil {
		return pageFetchResponse{}, fmt.Errorf("browser snapshot fetch page: %w", err)
	}

	return pageFetchResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       resp.Body,
	}, nil
}
