package app

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubRuntimeAlarmScheduler struct{}

func (s *stubRuntimeAlarmScheduler) Start(context.Context) {}

func TestBuildRuntimeAlarmScheduler(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{}

	t.Run("nil infra", func(t *testing.T) {
		scheduler := buildRuntimeAlarmScheduler(context.Background(), cfg, nil, logger)
		assert.Nil(t, scheduler)
	})

	t.Run("nil builder", func(t *testing.T) {
		scheduler := buildRuntimeAlarmScheduler(context.Background(), cfg, &coreInfrastructure{}, logger)
		assert.Nil(t, scheduler)
	})

	t.Run("builder provided", func(t *testing.T) {
		expected := &stubRuntimeAlarmScheduler{}
		called := false
		var infra *coreInfrastructure
		infra = &coreInfrastructure{
			runtimeAlarmSchedulerBuilder: func(ctx context.Context, gotCfg *config.Config, gotInfra *coreInfrastructure, gotLogger *slog.Logger) runtimeAlarmScheduler {
				called = true
				require.NotNil(t, ctx)
				assert.Same(t, cfg, gotCfg)
				assert.Same(t, infra, gotInfra)
				assert.Same(t, logger, gotLogger)
				return expected
			},
		}

		scheduler := buildRuntimeAlarmScheduler(context.Background(), cfg, infra, logger)
		assert.True(t, called)
		assert.Same(t, expected, scheduler)
	})
}
