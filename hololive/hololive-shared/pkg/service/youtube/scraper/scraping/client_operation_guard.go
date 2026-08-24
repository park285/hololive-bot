package scraping

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	youtubeadmission "github.com/kapu/hololive-shared/pkg/service/youtube/admission"
	parser "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
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
		return "", fmt.Errorf("ensure channel source allowed: %w", err)
	}

	html, err := c.fetchPage(ctx, pageURL, policy...)
	if err != nil {
		return "", errors.Join(c.handleChannelSourceFetchError(ctx, channelID, source, err))
	}

	if strings.TrimSpace(html) == "" {
		err := fmt.Errorf("%s empty response from %s", operation, pageURL)
		if failureErr := c.channelSourceFailure(ctx, channelID, source, err); failureErr != nil {
			return "", fmt.Errorf("%w", failureErr)
		}

		return "", nil
	}

	return html, nil
}

func (c *Client) handleChannelSourceFetchError(ctx context.Context, channelID string, source FailureSource, cause error) error {
	if youtubeadmission.IsDeferred(cause) {
		return fmt.Errorf("fetch page: %w", cause)
	}

	if err := c.channelSourceFailure(ctx, channelID, source, cause); err != nil {
		return fmt.Errorf("%w", err)
	}

	return nil
}

func (c *Client) channelSourceFailure(ctx context.Context, channelID string, source FailureSource, cause error) error {
	delay := c.recordChannelSourceFailure(ctx, channelID, ClassifyFailure(cause, source))
	if cooldownErr := channelSourceCooldownError(source, delay, cause); cooldownErr != nil {
		return fmt.Errorf("channel source cooldown error: %w", cooldownErr)
	}

	return nil
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

	delay := c.recordChannelSourceFailure(ctx, channelID, detail)

	if cooldownErr := channelSourceCooldownError(source, delay, err); cooldownErr != nil {
		return fmt.Errorf("channel source cooldown error: %w", cooldownErr)
	}

	return nil
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
