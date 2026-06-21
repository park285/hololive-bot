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

package milestonescheduler

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type staleAlertWork struct {
	notification string
	message      string
	key          string
}

func dispatchStaleAlertWorks(
	logger *slog.Logger,
	ledger *sentRoomLedger,
	sendMessage func(room, message string) error,
	rooms []string,
	works []staleAlertWork,
) []string {
	return dispatchAlertWorks(
		logger,
		context.Background(),
		ledger,
		sendMessage,
		rooms,
		works,
		func(w staleAlertWork) string { return w.notification },
		func(w staleAlertWork) string { return w.message },
		func(w staleAlertWork) string { return w.key },
		func(n string) string { return n },
		"send failed",
		"partial",
	)
}

func TestMilestoneLedger_StaleRoomsExcluded_d7d9140b(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var ledger sentRoomLedger

	const key = "m:UC1:sub:100000"
	works := []staleAlertWork{{notification: "A", message: "msg", key: key}}

	var mu sync.Mutex
	failRoom := "room-2"
	sendMessage := func(room, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		if room == failRoom {
			return assert.AnError
		}
		return nil
	}

	cycle1 := dispatchStaleAlertWorks(logger, &ledger, sendMessage, []string{"room-1", "room-2"}, works)
	require.Empty(t, cycle1, "cycle 1은 room-2 실패로 partial이어야 한다")
	require.Equal(t, 1, ledger.sentCount(key), "cycle 1 후 ledger에 room-1만 남아야 한다")

	cycle2 := dispatchStaleAlertWorks(logger, &ledger, sendMessage, []string{"room-2"}, works)
	assert.Empty(t, cycle2, "stale room-1이 현재 target room-2의 실패를 sent로 오인하면 안 된다")
}
