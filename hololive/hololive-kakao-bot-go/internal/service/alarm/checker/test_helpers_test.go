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

package checker

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/kapu/hololive-shared/pkg/service/cache"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
)

func newCheckerTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
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

	cacheSvc, err := cache.NewCacheService(
		context.Background(),
		cache.Config{
			Host:         host,
			Port:         port,
			DisableCache: true,
		},
		newCheckerTestLogger(),
	)
	if err != nil {
		t.Fatalf("NewCacheService() error = %v", err)
	}

	t.Cleanup(func() {
		_ = cacheSvc.Close()
		mini.Close()
	})

	return cacheSvc
}

func newCheckerTestAlarmService(t *testing.T, cacheSvc cache.Client) *notification.AlarmService {
	t.Helper()

	alarmSvc, err := notification.NewAlarmService(
		cacheSvc,
		nil,
		nil,
		nil,
		nil,
		nil,
		newCheckerTestLogger(),
		[]int{5, 3, 1},
	)
	if err != nil {
		t.Fatalf("NewAlarmService() error = %v", err)
	}
	t.Cleanup(func() {
		_ = alarmSvc.Close(context.Background())
	})

	return alarmSvc
}
