package workerapp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
)

func TestBuildAlarmDispatchRunnerRejectsInvalidShortLinkOrigin(t *testing.T) {
	config, state := alarmWorkerTestConfig(t)

	config.Notification.AlarmShortLinkBaseURL = "http://go.example.com"

	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	scheduler, err := buildAlarmDispatchRunner(t.Context(), config, infra, egress.NewIrisMessageSender(nil), nil, state)

	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "validate alarm dispatch short links")
}

func TestBuildAlarmDispatchRunnerAcceptsShortLinksWithRoomScopedKaring(t *testing.T) {
	config, state := alarmWorkerTestConfig(t)

	config.Notification.AlarmShortLinkBaseURL = "https://short.holoshi.com"

	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	scheduler, err := buildAlarmDispatchRunner(t.Context(), config, infra, egress.NewIrisMessageSender(nil), nil, state)

	require.NoError(t, err)
	assert.NotNil(t, scheduler)
}
