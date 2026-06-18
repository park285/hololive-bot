package polltarget

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newYouTubePollTargetTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newBufferedYouTubePollTargetTestLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, nil)), &buf
}

func schedulerJobKeys(t *testing.T, scheduler any) []string {
	t.Helper()

	inspector := requireSchedulerInspector(t, scheduler)
	return inspector.JobKeys()
}

type schedulerWakeSignal struct {
	scheduler schedulerInspector
}

func schedulerWakeCh(t *testing.T, scheduler any) schedulerWakeSignal {
	t.Helper()

	return schedulerWakeSignal{scheduler: requireSchedulerInspector(t, scheduler)}
}

type schedulerInspector interface {
	JobKeys() []string
	JobInterval(string) (time.Duration, bool)
	DrainWakeSignal() bool
}

func requireSchedulerInspector(t *testing.T, scheduler any) schedulerInspector {
	t.Helper()

	inspector, ok := scheduler.(schedulerInspector)
	require.True(t, ok, "scheduler must expose inspection methods")
	return inspector
}

func drainSchedulerWakeCh(ch schedulerWakeSignal) {
	ch.scheduler.DrainWakeSignal()
}

func requireNoSchedulerWakeSignal(t *testing.T, ch schedulerWakeSignal) {
	t.Helper()

	if ch.scheduler.DrainWakeSignal() {
		t.Fatal("expected no scheduler wake signal")
	}
}

func schedulerJobInterval(t *testing.T, scheduler any, key string) time.Duration {
	t.Helper()

	interval, ok := requireSchedulerInspector(t, scheduler).JobInterval(key)
	require.True(t, ok, "job %s must exist", key)
	return interval
}

type refreshTestPoller struct {
	name string
}

func (p refreshTestPoller) Poll(context.Context, string) error {
	return nil
}

func (p refreshTestPoller) Name() string {
	return p.name
}
