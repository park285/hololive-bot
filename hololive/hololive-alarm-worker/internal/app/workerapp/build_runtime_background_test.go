package workerapp

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	workerconfig "github.com/kapu/hololive-alarm-worker/internal/config"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/queue"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
)

// PostgreSQL이 없는 infra에서 켜진 runner를 조용히 끄지 않고 조립 오류로 드러낸다.
func TestBackgroundRunnersRequirePostgres(t *testing.T) {
	t.Parallel()

	appConfig := &workerconfig.RuntimeConfig{
		Celebration: workerconfig.CelebrationRunnerConfig{Enabled: true, RunInterval: time.Hour},
	}

	runners, stage, err := buildAlarmWorkerBackgroundRunners(
		appConfig, &sharedmodules.InfraModule{}, &alarmFoundation{}, queue.PublishConfig{}, slog.New(slog.DiscardHandler),
	)
	require.EqualError(t, err, "postgres is required")
	require.Equal(t, "background runners", stage)
	require.Equal(t, alarmWorkerBackgroundRunners{}, runners)
}
