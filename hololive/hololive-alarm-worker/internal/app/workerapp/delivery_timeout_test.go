package workerapp

import (
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
)

func TestDeliveryBuildersApplyProfileAttemptTimeout(t *testing.T) {
	cfg, state := alarmWorkerTestConfig(t)

	for _, workerID := range []string{"notification_delivery", "youtube_delivery"} {
		worker := cfg.AlarmWorkerProfile.Loaded.Profile.Workers[workerID]

		worker.Executor.AttemptTimeout.Milliseconds = new(int64(1500))
		cfg.AlarmWorkerProfile.Loaded.Profile.Workers[workerID] = worker
	}

	cfg.AlarmWorkerProfile.YouTubeDelivery.DeliverySendTimeoutMS = 1500

	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{pool: new(pgxpool.Pool)}}
	logger := slog.New(slog.DiscardHandler)
	notification, err := buildDeliveryOutboxDispatcher(cfg, infra, nil, logger, state)
	require.NoError(t, err)

	// builder가 반환한 실제 runner의 dispatcher 설정을 확인합니다. 외부 전송이나 DB 쿼리는 실행하지 않습니다.
	assembled := reflect.ValueOf(notification).FieldByName("dispatcher").Elem().Elem()
	require.Equal(t, 1500*time.Millisecond, time.Duration(assembled.FieldByName("config").FieldByName("AttemptTimeout").Int()))

	youtube, err := newYouTubeOutboxDispatcher(cfg, infra, nil, nil, logger)
	require.NoError(t, err)
	require.Equal(t, 1500*time.Millisecond, time.Duration(reflect.ValueOf(youtube).Elem().FieldByName("config").FieldByName("DeliverySendTimeout").Int()))
}

func TestYouTubeDeliveryBuilderRejectsConflictingTimeouts(t *testing.T) {
	cfg, _ := alarmWorkerTestConfig(t)

	cfg.AlarmWorkerProfile.YouTubeDelivery.DeliverySendTimeoutMS = 1500

	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{pool: new(pgxpool.Pool)}}
	_, err := newYouTubeOutboxDispatcher(cfg, infra, nil, nil, slog.New(slog.DiscardHandler))
	require.ErrorContains(t, err, "send timeout must match executor attempt timeout")
}
