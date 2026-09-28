package scraping

import (
	"context"
	"log/slog"
	"time"
)

// captureSnapshot은 정책 검사를 통과한 snapshot만 저장한다. 최소 간격 marker(youtube:producer:snapshot-interval:*)는
// 운영에서 store가 주입된 적이 없어 적용되지 않았으므로 지웠다(Valkey 책임 축소 A12).
func (c *Client) captureSnapshot(ctx context.Context, snapshot *Snapshot) {
	policy := c.snapshotPolicy
	if !c.shouldCaptureSnapshot(snapshot, policy) {
		return
	}

	normalizeSnapshotPayload(snapshot, policy)

	if len(snapshot.Body) == 0 {
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

func normalizeSnapshotPayload(snapshot *Snapshot, policy SnapshotPolicy) *Snapshot {
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

func (c *Client) shouldCaptureSnapshot(snapshot *Snapshot, policy SnapshotPolicy) bool {
	if c == nil || c.snapshotSink == nil {
		return false
	}

	return policy.allows(snapshot.Reason)
}
