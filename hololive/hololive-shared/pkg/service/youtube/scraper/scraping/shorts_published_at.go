package scraping

import (
	"context"
	"fmt"
	"time"

	parser "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

// 신선도 resolve는 best-effort 부가 조회다.
func (c *Client) GetShortPublishedAt(ctx context.Context, videoID string) (*time.Time, error) {
	url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)

	html, err := c.fetchPage(ctx, url, MetadataResolveFetchPolicy)
	if err != nil {
		return nil, fmt.Errorf("fetch short watch page %s: %w", videoID, err)
	}

	publishedAt, err := parser.ExtractPublishedAtFromHTML(html)
	if err != nil {
		return nil, fmt.Errorf("extract short published_at %s: %w", videoID, err)
	}

	return publishedAt, nil
}
