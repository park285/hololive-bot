package scraping

import (
	"context"
	"fmt"
	"time"
)

// 신선도 resolve는 best-effort 부가 조회라서 실패를 채널 헬스에 기록하지 않는다.
// 기록하면 /watch 한 번의 실패가 같은 채널의 본 쇼츠 스크랩까지 cooldown시킨다.
func (c *Client) GetShortPublishedAt(ctx context.Context, channelID, videoID string) (*time.Time, error) {
	if err := c.ensureChannelSourceAllowed(ctx, channelID, FailureSourceHTML); err != nil {
		return nil, fmt.Errorf("short watch page %s: %w", videoID, err)
	}

	url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", videoID)
	html, err := c.fetchPage(ctx, url, MetadataResolveFetchPolicy)
	if err != nil {
		return nil, fmt.Errorf("fetch short watch page %s: %w", videoID, err)
	}

	publishedAt, err := extractPublishedAtFromHTML(html)
	if err != nil {
		return nil, fmt.Errorf("extract short published_at %s: %w", videoID, err)
	}
	return publishedAt, nil
}
