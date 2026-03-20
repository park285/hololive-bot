// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package notification

import (
	"context"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/stretchr/testify/require"
)

type blockingAlarmWriter struct {
	started chan string
	block   chan struct{}
}

func (w *blockingAlarmWriter) Add(context.Context, *domain.Alarm) error {
	select {
	case w.started <- "add":
	default:
	}

	<-w.block

	return nil
}

func (w *blockingAlarmWriter) Remove(context.Context, string, string) error {
	select {
	case w.started <- "remove":
	default:
	}

	return nil
}

func (w *blockingAlarmWriter) ClearByRoom(context.Context, string) (int64, error) {
	select {
	case w.started <- "clear":
	default:
	}

	return 0, nil
}

func TestAlarmService_PersistWriteThrough_IsRoomKeyedSerialized(t *testing.T) {
	t.Parallel()

	writer := &blockingAlarmWriter{
		started: make(chan string, 2),
		block:   make(chan struct{}),
	}
	executor := newStripedExecutor(2, 8)
	as := &AlarmService{
		alarmWriter:     writer,
		persistExecutor: executor,
		logger:          newDiscardAlarmLogger(),
	}

	as.persistAlarmAsync(&domain.Alarm{
		RoomID:    "room-1",
		ChannelID: "ch-1",
	})

	select {
	case got := <-writer.started:
		require.Equal(t, "add", got)
	case <-time.After(1 * time.Second):
		t.Fatal("add persist did not start in time")
	}

	as.removeAlarmAsync("room-1", "ch-1")

	select {
	case got := <-writer.started:
		t.Fatalf("remove started early: got=%q", got)
	case <-time.After(80 * time.Millisecond):
	}

	close(writer.block)

	select {
	case got := <-writer.started:
		require.Equal(t, "remove", got)
	case <-time.After(1 * time.Second):
		t.Fatal("remove persist did not start in time")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 1*time.Second)
	defer cancel()

	require.NoError(t, executor.ShutdownWait(ctx))
}
