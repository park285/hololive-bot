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

package checking

import (
	"log/slog"
	"net"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	testChannelID1         = "channel-1"
	testChannelID2         = "channel-2"
	testChannelName1       = "Channel 1"
	testDBChannelName      = "DB Channel"
	testChID1              = "ch-1"
	testChIDLive           = "ch-live"
	testChIDShort1         = "ch1"
	testChIDShort2         = "ch2"
	testRoomID1            = "room-1"
	testRoomID2            = "room-2"
	testRoomID3            = "room-3"
	testRoomShort1         = "room1"
	testRoomShort2         = "room2"
	testStreamID1          = "stream-1"
	testLiveIncludedID     = "live-included"
	testUpcomingIncludedID = "upcoming-included"
	testUsersLivePath      = "/users/live"
)

func newCheckerTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newCheckerTestCacheClient(t *testing.T) cache.Client {
	t.Helper()

	mini := miniredis.RunT(t)

	host, rawPort, err := net.SplitHostPort(mini.Addr())
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}

	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatalf("Atoi() error = %v", err)
	}

	cacheClient, err := cache.NewCacheService(
		t.Context(),
		cache.Config{
			Host:              host,
			Port:              port,
			DisableCache:      true,
			ForceSingleClient: true,
		},
		newCheckerTestLogger(),
	)
	if err != nil {
		t.Fatalf("NewCacheService() error = %v", err)
	}

	t.Cleanup(func() {
		if err := cacheClient.Close(); err != nil {
			t.Errorf("cacheClient.Close() error = %v", err)
		}

		mini.Close()
	})

	return cacheClient
}
