package workerapp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
)

func TestBuildAlarmDispatchRunnerRejectsInvalidShortLinkOrigin(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "false")
	t.Setenv("ALARM_SHORT_LINK_BASE_URL", "http://go.example.com")

	config, state := alarmWorkerTestConfig(t)
	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	scheduler, err := buildAlarmDispatchRunner(t.Context(), config, infra, egress.NewIrisMessageSender(nil), nil, state)

	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "validate alarm dispatch short links")
}

func TestBuildAlarmDispatchRunnerRejectsShortLinksWithKaring(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "true")
	t.Setenv("ALARM_SHORT_LINK_BASE_URL", "https://short.holoshi.com")

	config, state := alarmWorkerTestConfig(t)
	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	scheduler, err := buildAlarmDispatchRunner(t.Context(), config, infra, egress.NewIrisMessageSender(nil), nil, state)

	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "ALARM_DISPATCH_KARING_ENABLED=false")
}
