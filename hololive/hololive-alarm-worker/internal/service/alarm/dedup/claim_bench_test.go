package dedup

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

// TestTryClaimKeyInfoLevelAllocationBudget는 운영 레벨(Info)에서 Debug 기록용 가명을 계산하지 않아
// claim 경로가 할당하지 않음을 지킨다. 할당 수가 race 빌드에서도 같아 build tag를 두지 않는다.
func TestTryClaimKeyInfoLevelAllocationBudget(t *testing.T) {
	key := keys.BuildNotifyClaimKey("18412345678901234", "stream-1", time.Unix(1785499200, 0).UTC(), "target")
	client := cachemocks.NewStrictClient()

	client.SetNXFunc = func(context.Context, string, string, time.Duration) (bool, error) {
		return true, nil
	}

	var sink bytes.Buffer

	logger := slog.New(slog.NewJSONHandler(&sink, &slog.HandlerOptions{Level: slog.LevelInfo}))
	service := NewService(client, []int{10}, logger)
	ctx := t.Context()

	allocs := testing.AllocsPerRun(1000, func() {
		acquired, err := service.tryClaimKey(ctx, key, time.Minute)
		if err != nil || !acquired {
			t.Fatalf("tryClaimKey = (%v, %v), want (true, nil)", acquired, err)
		}
	})
	if allocs > 0 {
		t.Errorf("tryClaimKey info level allocs/op = %.1f, want 0", allocs)
	}
}

// BenchmarkTryClaimKey는 Valkey 왕복을 뺀 claim 1건의 로그 인자 비용을 운영 레벨(Info)과
// Debug 레벨에서 각각 잰다.
func BenchmarkTryClaimKey(b *testing.B) {
	key := keys.BuildNotifyClaimKey("18412345678901234", "stream-1", time.Unix(1785499200, 0).UTC(), "target")

	for _, level := range []struct {
		name  string
		level slog.Level
	}{
		{name: "info", level: slog.LevelInfo},
		{name: "debug", level: slog.LevelDebug},
	} {
		b.Run(level.name, func(b *testing.B) {
			client := cachemocks.NewStrictClient()

			client.SetNXFunc = func(context.Context, string, string, time.Duration) (bool, error) {
				return true, nil
			}

			var sink bytes.Buffer

			logger := slog.New(slog.NewJSONHandler(&sink, &slog.HandlerOptions{Level: level.level}))
			service := NewService(client, []int{10}, logger)
			ctx := b.Context()

			b.ReportAllocs()

			for b.Loop() {
				sink.Reset()

				acquired, err := service.tryClaimKey(ctx, key, time.Minute)
				if err != nil || !acquired {
					b.Fatalf("tryClaimKey = (%v, %v), want (true, nil)", acquired, err)
				}
			}
		})
	}
}
