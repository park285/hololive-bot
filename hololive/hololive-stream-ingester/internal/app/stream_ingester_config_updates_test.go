package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/config"
	providers "github.com/kapu/hololive-shared/pkg/providers"
)

func TestProvideAPIAddr_DifferentPorts(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		port int
		want string
	}{
		"기본 포트":  {port: 30004, want: ":30004"},
		"커스텀 포트": {port: 8080, want: ":8080"},
		"0번 포트":  {port: 0, want: ":0"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := &config.Config{Server: config.ServerConfig{Port: tc.port}}
			assert.Equal(t, tc.want, ProvideAPIAddr(cfg))
		})
	}
}

func TestProvideYouTubeService_Nil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, ProvideYouTubeService(nil))
}

func TestProvideYouTubeService_ReturnsStackService(t *testing.T) {
	t.Parallel()

	svc := &fakeYouTubeService{}
	stack := &providers.YouTubeStack{Service: svc}

	got := ProvideYouTubeService(stack)
	require.NotNil(t, got)
	assert.Same(t, svc, got)
}

func TestApplyScraperProxyToggle_NilDeps(t *testing.T) {
	t.Parallel()

	// nil 의존성에서 패닉 없이 실행되어야 함
	assert.NotPanics(t, func() {
		applyScraperProxyToggle(true, nil, nil, nil, testLogger())
	})
}

func TestApplyScraperProxyToggle_EnableDisable(t *testing.T) {
	t.Parallel()

	svc := &fakeYouTubeService{}

	applyScraperProxyToggle(true, svc, nil, nil, testLogger())
	assert.Equal(t, 1, svc.setCalls)
	assert.True(t, svc.lastEnabled)

	applyScraperProxyToggle(false, svc, nil, nil, testLogger())
	assert.Equal(t, 2, svc.setCalls)
	assert.False(t, svc.lastEnabled)
}
