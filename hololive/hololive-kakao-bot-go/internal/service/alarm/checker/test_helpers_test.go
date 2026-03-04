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
