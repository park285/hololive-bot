package scraper

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type captureSink struct {
	snapshots []Snapshot
}

func (s *captureSink) Capture(_ context.Context, snapshot Snapshot) error {
	s.snapshots = append(s.snapshots, snapshot)
	return nil
}

func TestTrimSnapshotBody(t *testing.T) {
	body := strings.Repeat("a", 1024)
	got := trimSnapshotBody(body, 128)
	require.Len(t, got, 128)
}

func TestSnapshotPolicyAllowsOnlyConfiguredReason(t *testing.T) {
	policy := SnapshotPolicy{
		Enabled: true,
		AllowedReasons: map[FailureReason]bool{
			FailureReasonParserDrift: true,
		},
	}

	require.True(t, policy.allows(FailureReasonParserDrift))
	require.False(t, policy.allows(FailureReasonTransport))
}

func TestRecordParserDriftCapturesSnapshotWhenEnabled(t *testing.T) {
	sink := &captureSink{}
	client := NewClient(
		WithStateStore(newTestStateStore()),
		WithSnapshotSink(sink),
		WithSnapshotPolicy(SnapshotPolicy{
			Enabled:      true,
			MaxBodyBytes: 4,
			AllowedReasons: map[FailureReason]bool{
				FailureReasonParserDrift: true,
			},
		}),
	)

	err := client.recordParserDrift(context.Background(), "upcoming_events", "extract", "UC_TEST", "https://example.test", FailureSourceHTML, "abcdef", errors.New("marker missing"))

	require.Error(t, err)
	require.Len(t, sink.snapshots, 1)
	require.Equal(t, []byte("abcd"), sink.snapshots[0].Body)
	require.Equal(t, FailureReasonParserDrift, sink.snapshots[0].Reason)
}
