package scraping

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

const browserDiagnosticMinParserDriftFailures = 3

func (c *Client) CaptureBrowserDiagnosticSnapshot(ctx context.Context, channelID string, pageURL string) error {
	if !c.shouldCaptureBrowserDiagnostic(ctx, channelID) {
		return nil
	}
	snapshot := Snapshot{
		Operation:  "browser_diagnostic",
		ChannelID:  channelID,
		URL:        pageURL,
		Source:     FailureSourceBrowserSnapshot,
		Reason:     FailureReasonParserDrift,
		Stage:      "rendered_html",
		CapturedAt: time.Now().UTC(),
	}
	if !c.reserveSnapshotInterval(ctx, snapshot) {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("create browser diagnostic request: %w", err)
	}
	applyScraperHeaders(req, c.uaProvider.Headers(ctx))
	resp, err := c.browserSnapshotFetcher.FetchPage(ctx, pageFetchRequest{URL: pageURL, Header: req.Header})
	if err != nil {
		slog.Warn("browser diagnostic snapshot failed", "channel_id", channelID, "url", pageURL, "error", err)
		return err
	}
	snapshot.StatusCode = resp.StatusCode
	snapshot.Body = resp.Body
	c.captureSnapshotWithInterval(ctx, snapshot, false)
	return nil
}

func (c *Client) shouldCaptureBrowserDiagnostic(ctx context.Context, channelID string) bool {
	if c == nil || c.browserSnapshotFetcher == nil || c.channelHealth == nil {
		return false
	}
	if !c.snapshotPolicy.Enabled || c.snapshotSink == nil {
		return false
	}
	health, ok := c.channelHealth.Get(ctx, channelID, FailureSourceHTML)
	return ok &&
		health.LastFailureReason == FailureReasonParserDrift &&
		health.ConsecutiveFailures >= browserDiagnosticMinParserDriftFailures
}
