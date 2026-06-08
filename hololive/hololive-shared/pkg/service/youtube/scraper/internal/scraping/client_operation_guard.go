package scraping

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func (c *Client) ensureChannelSourceAllowed(ctx context.Context, channelID string, source FailureSource) error {
	if c == nil || c.channelHealth == nil {
		return nil
	}
	wait, ok := c.channelHealth.ShouldSkip(ctx, channelID, source, time.Now())
	if !ok {
		return nil
	}
	return &CooldownError{Kind: fmt.Sprintf("youtube channel-source %s", source), Delay: wait, Err: ErrTransientCooldown}
}

func (c *Client) fetchChannelSourcePage(ctx context.Context, operation, channelID, pageURL string, source FailureSource, policy ...FetchPolicy) (string, error) {
	if err := c.ensureChannelSourceAllowed(ctx, channelID, source); err != nil {
		return "", err
	}
	html, err := c.fetchPage(ctx, pageURL, policy...)
	if err != nil {
		if IsAdmissionDeferred(err) {
			return "", err
		}
		delay := c.recordChannelSourceFailure(ctx, channelID, ClassifyFailure(err, source))
		return "", channelSourceCooldownError(source, delay, err)
	}
	if strings.TrimSpace(html) == "" {
		err := fmt.Errorf("%s empty response from %s", operation, pageURL)
		delay := c.recordChannelSourceFailure(ctx, channelID, ClassifyFailure(err, source))
		return "", channelSourceCooldownError(source, delay, err)
	}
	return html, nil
}

func (c *Client) recordChannelSourceSuccess(ctx context.Context, channelID string, source FailureSource) {
	if c == nil || c.channelHealth == nil {
		return
	}
	c.channelHealth.RecordSuccess(ctx, channelID, source, time.Now())
}

func (c *Client) recordChannelSourceFailure(ctx context.Context, channelID string, detail FailureDetail) time.Duration {
	if c == nil || c.channelHealth == nil {
		return 0
	}
	return c.channelHealth.RecordFailure(ctx, channelID, detail, time.Now())
}

func (c *Client) recordParserDrift(ctx context.Context, operation, stage, channelID, pageURL string, source FailureSource, html string, cause error) error {
	err := NewParserDriftError(operation, stage, cause)
	detail := ClassifyFailure(err, source)
	c.captureSnapshot(ctx, Snapshot{
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
	delay := c.recordChannelSourceFailure(ctx, channelID, detail)
	return channelSourceCooldownError(source, delay, err)
}

func channelSourceCooldownError(source FailureSource, delay time.Duration, err error) error {
	if delay <= 0 || err == nil {
		return err
	}
	return &CooldownError{
		Kind:  fmt.Sprintf("youtube channel-source %s", source),
		Delay: delay,
		Err:   err,
	}
}
