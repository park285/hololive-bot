package scraping

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

func (c *Client) captureSnapshot(ctx context.Context, snapshot Snapshot) {
	c.captureSnapshotWithInterval(ctx, snapshot, true)
}

func (c *Client) captureSnapshotWithInterval(ctx context.Context, snapshot Snapshot, checkInterval bool) {
	policy := c.snapshotPolicy
	if !c.shouldCaptureSnapshot(snapshot, policy) {
		return
	}
	snapshot = normalizeSnapshotPayload(snapshot, policy)
	if len(snapshot.Body) == 0 {
		return
	}
	if checkInterval && !c.allowSnapshotInterval(ctx, snapshot, policy.MinInterval) {
		return
	}
	if err := c.snapshotSink.Capture(ctx, snapshot); err != nil {
		slog.Warn("failed to capture youtube producer snapshot",
			"operation", snapshot.Operation,
			"channel_id", snapshot.ChannelID,
			"source", snapshot.Source,
			"reason", snapshot.Reason,
			"stage", snapshot.Stage,
			"error", err)
	}
}

func normalizeSnapshotPayload(snapshot Snapshot, policy SnapshotPolicy) Snapshot {
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now().UTC()
	}
	if snapshot.SchemaVersion == "" {
		snapshot.SchemaVersion = SnapshotSchemaVersion
	}
	if policy.MaxBodyBytes > 0 && len(snapshot.Body) > policy.MaxBodyBytes {
		snapshot.Body = snapshot.Body[:policy.MaxBodyBytes]
	}
	return snapshot
}

func (c *Client) shouldCaptureSnapshot(snapshot Snapshot, policy SnapshotPolicy) bool {
	if c == nil || c.snapshotSink == nil {
		return false
	}
	return policy.allows(snapshot.Reason)
}

func (c *Client) allowSnapshotInterval(ctx context.Context, snapshot Snapshot, interval time.Duration) bool {
	if interval <= 0 || c == nil || c.stateStore == nil {
		return true
	}
	key := snapshotIntervalStateKey(snapshot)
	var marker bool
	if err := c.stateStore.Get(ctx, key, &marker); err == nil && marker {
		return false
	}
	if err := c.stateStore.Set(ctx, key, true, interval); err != nil {
		slog.Warn("failed to persist youtube producer snapshot interval marker", "key", key, "error", err)
	}
	return true
}

func (c *Client) reserveSnapshotInterval(ctx context.Context, snapshot Snapshot) bool {
	return c.allowSnapshotInterval(ctx, snapshot, c.snapshotPolicy.MinInterval)
}

func snapshotIntervalStateKey(snapshot Snapshot) string {
	return fmt.Sprintf("youtube:producer:snapshot-interval:%s:%s:%s:%s",
		strings.TrimSpace(snapshot.Operation),
		strings.TrimSpace(snapshot.ChannelID),
		strings.TrimSpace(snapshot.Stage),
		strings.TrimSpace(string(snapshot.Reason)))
}
