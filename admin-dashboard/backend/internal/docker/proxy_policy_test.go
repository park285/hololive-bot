package docker

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminProxyRejectsOutOfScopeActions(t *testing.T) {
	data, err := os.ReadFile("../../../../deploy/compose/docker-compose.admin-security.yml")
	require.NoError(t, err)

	var patterns []string

	for line := range strings.SplitSeq(string(data), "\n") {
		if pattern, ok := strings.CutPrefix(strings.TrimSpace(line), "- '-allowPOST="); ok {
			patterns = append(patterns, strings.TrimSuffix(pattern, "'"))
		}
	}

	require.Len(t, patterns, 1)

	// socket-proxy 1.12.3은 허용식 앞뒤에 앵커를 추가해 전체 요청 경로를 검사한다.
	allowed, err := regexp.Compile("^(?:" + patterns[0] + ")$")
	require.NoError(t, err)

	for _, target := range []string{"hololive-api", "hololive-alarm-worker", "hololive-youtube-collector-c"} {
		for _, action := range []string{"start", "stop", "restart"} {
			require.True(t, allowed.MatchString("/containers/"+target+"/"+action))
		}
	}

	for _, target := range []string{"holo-postgres", "valkey-cache", "deunhealth", "admin-dashboard", "admin-dashboard-ingress", "admin-docker-proxy"} {
		require.True(t, allowed.MatchString("/v1.51/containers/"+target+"/restart?t=30"))
		require.True(t, allowed.MatchString("/containers/"+target+"/start"))
		require.False(t, allowed.MatchString("/containers/"+target+"/stop"))
	}

	for _, path := range []string{
		"/containers/0123456789abcdef/restart", "/containers/unmanaged/start",
		"/containers/hololive-db-migrate/start", "/containers/hololive-api-init/start",
		"/containers/hololive-api-extra/start", "/containers/hololive-api/exec",
		"/containers/create", "/containers/hololive-api/restart/extra",
		"/containers/hololive-api/../unmanaged/restart", "/containers/hololive-api%2f..%2funmanaged/restart",
	} {
		require.False(t, allowed.MatchString(path), path)
	}
}
