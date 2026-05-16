package scraping

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/internal/browserfetcher"
)

type BrowserSnapshotConfig = browserfetcher.Config

type BrowserSnapshotFetcher struct {
	inner *browserfetcher.Fetcher
}

func NewBrowserSnapshotFetcher(endpoint string, timeout time.Duration) *BrowserSnapshotFetcher {
	return &BrowserSnapshotFetcher{inner: browserfetcher.New(endpoint, timeout)}
}

func (f *BrowserSnapshotFetcher) FetchPage(ctx context.Context, req pageFetchRequest) (pageFetchResponse, error) {
	if f == nil || f.inner == nil {
		return pageFetchResponse{}, fmt.Errorf("browser snapshot endpoint is not configured")
	}
	resp, err := f.inner.FetchPage(ctx, browserfetcher.Request{
		URL:    req.URL,
		Header: req.Header,
	})
	return pageFetchResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       resp.Body,
	}, err
}
