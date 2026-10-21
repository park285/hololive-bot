package youtubedispatch

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestEnqueueDeliveries_DoesNotLogZeroWorkAtInfo(t *testing.T) {
	t.Parallel()

	var logBuffer bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	dispatcher := newDispatcherForTest(t, nil, nil, &testSender{failRoom: map[string]bool{}}, nil, logger, &Config{})

	dispatcher.claim.recordOutboxEnqueueStats(0, outboxEnqueueStats{})

	if strings.Contains(logBuffer.String(), "Outbox per-room enqueue completed") {
		t.Fatalf("unexpected zero-work enqueue log: %s", logBuffer.String())
	}
}
