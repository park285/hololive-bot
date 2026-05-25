package polltarget

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"
	"unsafe"

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

	field := reflect.ValueOf(scheduler).Elem().FieldByName("jobMap")
	require.True(t, field.IsValid(), "jobMap field must exist")
	field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()

	keys := make([]string, 0, field.Len())
	iterator := field.MapRange()
	for iterator.Next() {
		keys = append(keys, iterator.Key().String())
	}

	return keys
}

func schedulerWakeCh(t *testing.T, scheduler any) chan struct{} {
	t.Helper()

	field := reflect.ValueOf(scheduler).Elem().FieldByName("wakeCh")
	require.True(t, field.IsValid(), "wakeCh field must exist")
	field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()

	wakeCh, ok := field.Interface().(chan struct{})
	require.True(t, ok, "wakeCh must be chan struct{}")
	return wakeCh
}

func drainSchedulerWakeCh(ch chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func requireNoSchedulerWakeSignal(t *testing.T, ch chan struct{}) {
	t.Helper()

	select {
	case <-ch:
		t.Fatal("expected no scheduler wake signal")
	default:
	}
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
