package docker

import (
	jsonv2 "encoding/json/v2"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminProxyRejectsOutOfScopeActions(t *testing.T) {
	data, err := os.ReadFile("../../../../../deploy/compose/admin-docker-policy.generated.json")
	require.NoError(t, err)

	var config struct {
		Services map[string]struct {
			Command []string `json:"command"`
		} `json:"services"`
	}

	require.NoError(t, jsonv2.Unmarshal(data, &config))

	var patterns []string

	for _, argument := range config.Services["admin-docker-proxy"].Command {
		if pattern, ok := strings.CutPrefix(argument, "-allowPOST="); ok {
			patterns = append(patterns, pattern)
		}
	}

	require.Len(t, patterns, 1)

	// socket-proxy 1.12.3은 앵커를 붙이고 r.URL.Path만 검사하며 query는 검사하지 않는다.
	allowed, err := regexp.Compile("^(?:" + patterns[0] + ")$")
	require.NoError(t, err)

	for _, target := range []string{testBusinessContainerName, "hololive-alarm-worker", "hololive-youtube-collector-c"} {
		for _, action := range []string{testStartAction, testStopAction, testRestartAction} {
			require.True(t, allowed.MatchString("/containers/"+target+"/"+action))
		}
	}

	for _, target := range []string{"holo-postgres", "valkey-cache", "deunhealth", testAdminContainerName, "admin-dashboard-ingress", "admin-docker-proxy"} {
		require.True(t, allowed.MatchString("/v1.51/containers/"+target+"/restart"))
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
