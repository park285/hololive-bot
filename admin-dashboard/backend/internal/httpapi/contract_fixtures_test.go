package httpapi

import (
	jsonv2 "encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/adapters/docker"
	"github.com/kapu/admin-dashboard/internal/observations"
)

// JSON Schema 검사기는 이 테스트의 비밀 없는 수동 DTO 출력으로 실제 직렬화 형태를 대조한다.
func TestContractResponseFixtures(t *testing.T) {
	policy := sessionPolicy{HeartbeatIntervalMS: 300000, IdleTimeoutMS: 600000, IdleWarningTimeoutMS: 540000, IdleSessionTTLMS: 10000, AbsoluteWarningWindowMS: 300000}
	fixtures := map[string][]any{
		"StatusOnlyResponse":    {statusResponse{Status: "ok"}, dockerActionResponse{Status: "ok", Message: "restarted"}},
		"LoginResponse":         {loginResponse{Status: "ok", Message: "Login successful", CSRFToken: "fixture-token"}},
		"SessionPolicyResponse": {policy},
		"SessionStatusResponse": {sessionStatusResponse{Status: "ok", Authenticated: true, Username: "admin", AbsoluteExpiresAt: 2000000000, SessionPolicy: policy, CSRFToken: "fixture-token"}},
		"HeartbeatResponse": {
			heartbeatOKResponse{Status: "ok", AbsoluteExpiresAt: 2000000000},
			heartbeatIdleResponse{Status: "idle", IdleRejected: true},
			heartbeatRotatedResponse{Status: "ok", Rotated: true, AbsoluteExpiresAt: 2000000000, CSRFToken: "fixture-token"},
		},
		"DockerHealthResponse":        {dockerHealthResponse{Status: "ok", Available: false}},
		"DockerContainerListResponse": {dockerContainersResponse{Status: "ok", Containers: []docker.Container{{ID: "fixture", Name: "hololive-api", State: "running", Status: "Up 2 hours", Image: "fixture", Managed: true}}}},
		"AggregatedStatus":            {observations.AggregatedStatus{Services: []observations.ServiceStatus{{Name: "fixture", Available: false}}, Uptime: "1m", Version: "test", SampledAt: 1700000000000}},
		"SystemStats":                 {observations.SystemStats{SampledAt: 1700000000000, ServiceRuntime: []observations.ServiceRuntimeStats{{Name: "fixture", MetricKind: observations.RuntimeMetricGoroutine}}}},
	}

	encoded, err := jsonv2.Marshal(fixtures, jsonv2.Deterministic(true))
	require.NoError(t, err)
	t.Logf("CONTRACT_FIXTURES %s", encoded)
}
