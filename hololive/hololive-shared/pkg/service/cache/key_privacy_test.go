package cache

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/internal/testredis"
)

const (
	privacyRoomID     = "상대방닉네임 님과의 대화"
	privacyStreamID   = "dQw4w9WgXcQ"
	privacyClaimKey   = "notified:claim:" + privacyRoomID + ":" + privacyStreamID + ":1785499200:10m"
	privacyRoomKey    = "alarm:" + privacyRoomID
	privacyRoomHash   = "alarm:room_names"
	privacyOutageWait = 2 * time.Second
)

// 이 경로의 회귀는 Valkey 장애 중에만 드러난다. 따라서 miniredis를 세운 뒤 끊어야 Service가 실제 실패 로그를
// 찍는 상태에 들어간다.
func outageService(t *testing.T) (*Service, *bytes.Buffer) {
	t.Helper()

	host, port, mini := testredis.StartMiniRedis(t)

	var sink bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&sink, &slog.HandlerOptions{Level: slog.LevelDebug}))

	service, err := NewCacheService(t.Context(), Config{
		Host:              host,
		Port:              port,
		DisableCache:      true,
		ForceSingleClient: true,
	}, logger)
	if err != nil {
		t.Fatalf("NewCacheService: %v", err)
	}

	t.Cleanup(func() {
		if err := service.Close(); err != nil {
			t.Errorf("close cache service: %v", err)
		}
	})

	mini.Close()
	sink.Reset()

	return service, &sink
}

func TestCacheOutageLogsNeverCarryRoomPlaintext(t *testing.T) {
	service, sink := outageService(t)

	ctx, cancel := context.WithTimeout(t.Context(), privacyOutageWait)
	defer cancel()

	if _, err := service.SetNX(ctx, privacyClaimKey, "1", time.Minute); err == nil {
		t.Fatal("SetNX: expected an outage error")
	}

	if err := service.Get(ctx, privacyRoomKey, nil); err == nil {
		t.Fatal("Get: expected an outage error")
	}

	if err := service.HSet(ctx, privacyRoomHash, privacyRoomID, "방 이름"); err == nil {
		t.Fatal("HSet: expected an outage error")
	}

	records := sink.String()
	if records == "" {
		t.Fatal("no log record was produced; the outage fixture is not exercising the failure path")
	}

	if strings.Contains(records, privacyRoomID) {
		t.Errorf("room plaintext reached the log sink:\n%s", records)
	}

	for _, want := range []string{"notified:claim:", "alarm:", privacyStreamID, privacyRoomHash} {
		if !strings.Contains(records, want) {
			t.Errorf("log records lost the diagnostic fragment %q:\n%s", want, records)
		}
	}
}

func TestCacheErrorStringNeverCarriesRoomPlaintext(t *testing.T) {
	t.Parallel()

	cause := errors.New("dial tcp 127.0.0.1:6379: connect: connection refused")
	err := NewCacheError("setnx", privacyClaimKey, cause)

	message := err.Error()
	if strings.Contains(message, privacyRoomID) {
		t.Errorf("CacheError.Error() = %q leaks the room identifier", message)
	}

	if !strings.Contains(message, "notified:claim:") || !strings.Contains(message, privacyStreamID) {
		t.Errorf("CacheError.Error() = %q lost the diagnostic key prefix or tail", message)
	}

	if !errors.Is(err, cause) {
		t.Error("CacheError no longer unwraps to its cause")
	}

	if err.Key != privacyClaimKey {
		t.Errorf("CacheError.Key = %q, want the raw key for programmatic callers", err.Key)
	}
}
