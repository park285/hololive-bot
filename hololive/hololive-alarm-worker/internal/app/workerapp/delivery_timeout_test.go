package workerapp

import (
	"log/slog"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
)

func TestYouTubeDeliveryBuilderAppliesProfileAttemptTimeout(t *testing.T) {
	cfg, _ := alarmWorkerTestConfig(t)

	worker := cfg.AlarmWorkerProfile.Loaded.Profile.Workers["youtube_delivery"]

	worker.Executor.AttemptTimeout.Milliseconds = new(int64(1500))
	cfg.AlarmWorkerProfile.Loaded.Profile.Workers["youtube_delivery"] = worker

	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{pool: new(pgxpool.Pool)}}
	logger := slog.New(slog.DiscardHandler)
	sender := egress.NewIrisMessageSender(nil)

	youtube, err := newYouTubeOutboxDispatcher(cfg, infra, sender, nil, logger)
	require.NoError(t, err)
	require.Equal(t, 1500*time.Millisecond, time.Duration(reflect.ValueOf(youtube).Elem().FieldByName("config").FieldByName("DeliverySendTimeout").Int()))
}

func TestYouTubeDeliveryBuilderDerivesParentFreshness(t *testing.T) {
	cfg, _ := alarmWorkerTestConfig(t)
	worker := cfg.AlarmWorkerProfile.Loaded.Profile.Workers["youtube_delivery"]

	worker.Queue.MaxAge.Milliseconds = new(int64(5400000))
	cfg.AlarmWorkerProfile.Loaded.Profile.Workers["youtube_delivery"] = worker

	config := youtubeDispatchConfig(cfg)
	require.Equal(t, 90*time.Minute, config.ClaimFreshnessWindow)
}
