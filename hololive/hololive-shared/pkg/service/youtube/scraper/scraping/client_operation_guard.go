package scraping

import (
	"context"
	"fmt"
	"strings"
	"time"

	parser "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

// 채널 source별 health/cooldown 저장 계층은 운영에서 store가 주입된 적이 없어 항상 no-op이었으므로 지웠다
// (Valkey 책임 축소 A12). 실패는 cooldown 없이 호출자에게 그대로 돌려준다.
func (c *Client) fetchChannelSourcePage(ctx context.Context, operation, pageURL string, policy ...FetchPolicy) (string, error) {
	html, err := c.fetchPage(ctx, pageURL, policy...)
	if err != nil {
		return "", fmt.Errorf("fetch page: %w", err)
	}

	if strings.TrimSpace(html) == "" {
		return "", fmt.Errorf("%s empty response from %s", operation, pageURL)
	}

	return html, nil
}

// recordParserDrift는 parser drift를 snapshot 정책에 따라 기록하고 drift 오류를 돌려준다.
func (c *Client) recordParserDrift(ctx context.Context, operation, stage, channelID, pageURL string, source FailureSource, html string, cause error) error {
	err := parser.NewParserDriftError(operation, stage, cause)
	detail := ClassifyFailure(err, source)
	c.captureSnapshot(ctx, &Snapshot{
		Operation:     operation,
		ChannelID:     channelID,
		URL:           pageURL,
		Source:        source,
		Reason:        detail.Reason,
		Stage:         stage,
		StatusCode:    detail.StatusCode,
		Body:          trimSnapshotBody(html, c.snapshotPolicy.MaxBodyBytes),
		CapturedAt:    time.Now().UTC(),
		SchemaVersion: SnapshotSchemaVersion,
	})

	return fmt.Errorf("record parser drift: %w", err)
}
