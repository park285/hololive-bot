package holodexprovider

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestPhotoSyncRunPeriodicSyncLogsStoppedWhenContextCanceled(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	ps := &PhotoSyncService{
		logger:       slog.New(slog.NewTextHandler(&logs, nil)),
		syncInterval: time.Hour,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ps.runPeriodicSync(ctx)

	if !strings.Contains(logs.String(), "Photo sync service stopped") {
		t.Fatalf("runPeriodicSync() log = %q, want stop message", logs.String())
	}
}
