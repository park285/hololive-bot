package workerapp

import (
	"testing"

	"github.com/kapu/hololive-alarm-worker/internal/egress"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildAlarmDispatchRunnerRejectsInvalidShortLinkOrigin(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "true")
	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "false")
	t.Setenv("ALARM_SHORT_LINK_BASE_URL", "http://go.example.com")
	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	scheduler, err := buildAlarmDispatchRunner(infra, egress.NewIrisMessageSender(nil), nil)

	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "validate alarm dispatch short links")
}

func TestBuildAlarmDispatchRunnerRejectsShortLinksWithKaring(t *testing.T) {
	t.Setenv("ALARM_DISPATCH_CONSUMER_ENABLED", "true")
	t.Setenv("ALARM_DISPATCH_KARING_ENABLED", "true")
	t.Setenv("ALARM_SHORT_LINK_BASE_URL", "https://short.holoshi.com")
	infra := &sharedmodules.InfraModule{Postgres: workerappEgressTestPostgres{}}

	scheduler, err := buildAlarmDispatchRunner(infra, egress.NewIrisMessageSender(nil), nil)

	require.Error(t, err)
	assert.Nil(t, scheduler)
	assert.Contains(t, err.Error(), "ALARM_DISPATCH_KARING_ENABLED=false")
}
