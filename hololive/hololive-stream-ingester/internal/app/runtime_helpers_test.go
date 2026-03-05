package app

import (
	"context"
	"net/http"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	sharedlogging "github.com/kapu/hololive-shared/pkg/logging"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
)

type fakeYouTubeService struct {
	setCalls     int
	lastEnabled  bool
	proxyEnabled bool
}

func (f *fakeYouTubeService) SetScraperProxyEnabled(enabled bool) bool {
	f.setCalls++
	f.lastEnabled = enabled
	f.proxyEnabled = enabled
	return true
}

func (f *fakeYouTubeService) ScraperProxyEnabled() bool { return f.proxyEnabled }

func (f *fakeYouTubeService) GetChannelStatistics(context.Context, []string) (map[string]*youtube.ChannelStats, error) {
	return map[string]*youtube.ChannelStats{}, nil
}

func (f *fakeYouTubeService) GetRecentVideos(context.Context, string, int64) ([]string, error) {
	return []string{}, nil
}

type fakeScheduler struct {
	startCalls int
	stopCalls  int
}

func (f *fakeScheduler) Start(context.Context) { f.startCalls++ }
func (f *fakeScheduler) Stop()                 { f.stopCalls++ }

var testLogger = sharedlogging.NewLogger

func TestProvideAPIAddr(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{Server: config.ServerConfig{Port: 30004}}
	if got := ProvideAPIAddr(cfg); got != ":30004" {
		t.Fatalf("ProvideAPIAddr() = %q, want %q", got, ":30004")
	}
}

func TestProvideYouTubeService(t *testing.T) {
	t.Parallel()

	t.Run("nil stack", func(t *testing.T) {
		if got := ProvideYouTubeService(nil); got != nil {
			t.Fatalf("ProvideYouTubeService(nil) = %#v, want nil", got)
		}
	})

	t.Run("returns stack service", func(t *testing.T) {
		service := &fakeYouTubeService{}
		stack := &providers.YouTubeStack{Service: service}

		got := ProvideYouTubeService(stack)
		if got == nil {
			t.Fatal("ProvideYouTubeService() returned nil")
		}
		if got != service {
			t.Fatalf("ProvideYouTubeService() = %#v, want %#v", got, service)
		}
	})
}

func TestApplyScraperProxyToggle(t *testing.T) {
	t.Parallel()

	t.Run("applies toggle to youtube service", func(t *testing.T) {
		service := &fakeYouTubeService{}

		applyScraperProxyToggle(true, service, nil, nil, testLogger())
		if service.setCalls != 1 {
			t.Fatalf("SetScraperProxyEnabled calls = %d, want 1", service.setCalls)
		}
		if !service.lastEnabled {
			t.Fatal("lastEnabled = false, want true")
		}

		applyScraperProxyToggle(false, service, nil, nil, testLogger())
		if service.setCalls != 2 {
			t.Fatalf("SetScraperProxyEnabled calls = %d, want 2", service.setCalls)
		}
		if service.lastEnabled {
			t.Fatal("lastEnabled = true, want false")
		}
	})

	t.Run("nil dependencies do not panic", func(t *testing.T) {
		applyScraperProxyToggle(true, nil, nil, nil, testLogger())
	})
}

func TestStreamIngesterRuntimeClose(t *testing.T) {
	t.Parallel()

	t.Run("nil runtime", func(t *testing.T) {
		var runtime *StreamIngesterRuntime
		runtime.Close()
	})

	t.Run("invokes cleanup once", func(t *testing.T) {
		calls := 0
		runtime := &StreamIngesterRuntime{
			cleanup: func() { calls++ },
		}

		runtime.Close()
		if calls != 1 {
			t.Fatalf("cleanup calls = %d, want 1", calls)
		}
	})
}

func TestStreamIngesterRuntimeShutdown(t *testing.T) {
	t.Parallel()

	scheduler := &fakeScheduler{}
	runtime := &StreamIngesterRuntime{
		Logger:    testLogger(),
		Scheduler: scheduler,
		HttpServer: &http.Server{
			Addr:    "invalid-address",
			Handler: http.NewServeMux(),
		},
	}

	runtime.shutdown()
	if scheduler.stopCalls != 1 {
		t.Fatalf("scheduler Stop calls = %d, want 1", scheduler.stopCalls)
	}
}

func TestStreamIngesterRuntimeRunStopsSchedulerOnServerError(t *testing.T) {
	t.Parallel()

	scheduler := &fakeScheduler{}
	runtime := &StreamIngesterRuntime{
		Logger:     testLogger(),
		Scheduler:  scheduler,
		ServerAddr: "invalid-address",
		HttpServer: &http.Server{
			Addr:    "invalid-address",
			Handler: http.NewServeMux(),
		},
	}

	runtime.Run()

	if scheduler.startCalls != 1 {
		t.Fatalf("scheduler Start calls = %d, want 1", scheduler.startCalls)
	}
	if scheduler.stopCalls != 1 {
		t.Fatalf("scheduler Stop calls = %d, want 1", scheduler.stopCalls)
	}
}

var _ youtube.Service = (*fakeYouTubeService)(nil)
var _ youtube.Scheduler = (*fakeScheduler)(nil)
