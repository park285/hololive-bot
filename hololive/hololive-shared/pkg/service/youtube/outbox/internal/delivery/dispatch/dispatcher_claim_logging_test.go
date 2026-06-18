package dispatch

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestEnqueueDeliveries_DoesNotLogZeroWorkAtInfo(t *testing.T) {
	t.Parallel()

	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	dispatcher := NewDispatcher(nil, nil, &testSender{failRoom: map[string]bool{}}, nil, logger, &Config{})

	dispatcher.claim.enqueueDeliveries(context.Background(), nil, map[string]channelAlarmRoomTargets{})

	if strings.Contains(logBuffer.String(), "Outbox per-room enqueue completed") {
		t.Fatalf("unexpected zero-work enqueue log: %s", logBuffer.String())
	}
}
