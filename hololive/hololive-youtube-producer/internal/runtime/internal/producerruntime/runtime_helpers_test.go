// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package producerruntime

import (
	"context"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	sharedsettings "github.com/kapu/hololive-shared/pkg/server/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	sharedlogging "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/lifecycle"
)

type fakeYouTubeService struct {
	mu           sync.Mutex
	setCalls     int
	lastEnabled  bool
	proxyEnabled bool
}

func (f *fakeYouTubeService) SetScraperProxyEnabled(enabled bool) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setCalls++
	f.lastEnabled = enabled
	f.proxyEnabled = enabled
	return true
}

func (f *fakeYouTubeService) ScraperProxyEnabled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.proxyEnabled
}

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

func schedulerJobKeys(t *testing.T, scheduler any) []string {
	t.Helper()

	field := reflect.ValueOf(scheduler).Elem().FieldByName("jobMap")
	if !field.IsValid() {
		t.Fatal("jobMap field must exist")
	}
	field = reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()

	keys := make([]string, 0, field.Len())
	iterator := field.MapRange()
	for iterator.Next() {
		keys = append(keys, iterator.Key().String())
	}

	return keys
}

func TestApplyScraperProxyToggle(t *testing.T) {
	t.Parallel()

	t.Run("applies toggle to youtube service", func(t *testing.T) {
		service := &fakeYouTubeService{}

		sharedsettings.ApplyScraperProxyToggle(true, service, nil, nil, testLogger())
		if service.setCalls != 1 {
			t.Fatalf("SetScraperProxyEnabled calls = %d, want 1", service.setCalls)
		}
		if !service.lastEnabled {
			t.Fatal("lastEnabled = false, want true")
		}

		sharedsettings.ApplyScraperProxyToggle(false, service, nil, nil, testLogger())
		if service.setCalls != 2 {
			t.Fatalf("SetScraperProxyEnabled calls = %d, want 2", service.setCalls)
		}
		if service.lastEnabled {
			t.Fatal("lastEnabled = true, want false")
		}
	})

	t.Run("nil dependencies do not panic", func(t *testing.T) {
		sharedsettings.ApplyScraperProxyToggle(true, nil, nil, nil, testLogger())
	})
}

func TestYouTubeProducerRuntimeClose(t *testing.T) {
	t.Parallel()

	t.Run("invokes cleanup once", func(t *testing.T) {
		calls := 0
		runtime := &YouTubeProducerRuntime{
			Managed: lifecycle.NewManaged(func() { calls++ }),
		}

		runtime.Close()
		if calls != 1 {
			t.Fatalf("cleanup calls = %d, want 1", calls)
		}
	})
}

func TestYouTubeProducerRuntimeShutdown(t *testing.T) {
	t.Parallel()

	scheduler := &fakeScheduler{}
	runtime := &YouTubeProducerRuntime{
		Logger:    testLogger(),
		Scheduler: scheduler,
		HttpServer: &http.Server{
			Addr:    "invalid-address",
			Handler: http.NewServeMux(),
		},
	}

	runtime.shutdown(context.Background())
	if scheduler.stopCalls != 1 {
		t.Fatalf("scheduler Stop calls = %d, want 1", scheduler.stopCalls)
	}
}

func TestYouTubeProducerRuntimeStartHTTPServerSendsListenError(t *testing.T) {
	t.Parallel()

	runtime := &YouTubeProducerRuntime{
		Logger:     testLogger(),
		ServerAddr: "invalid::addr",
		HttpServer: &http.Server{
			Addr: "invalid::addr",
		},
	}
	errCh := make(chan error, 1)

	runtime.startHTTPServer(errCh)

	select {
	case err := <-errCh:
		if err == nil || !strings.Contains(err.Error(), "http server error") {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for HTTP server error")
	}
}

func TestYouTubeProducerRuntimeRunStopsSchedulerOnServerError(t *testing.T) {
	t.Parallel()

	scheduler := &fakeScheduler{}
	readiness := newReadinessState(youtubeProducerRuntimeName, ingestionRuntimeFeatures{
		youtubeEnabled:   true,
		photoSyncEnabled: false,
	})
	runtime := &YouTubeProducerRuntime{
		Logger:     testLogger(),
		Scheduler:  scheduler,
		ServerAddr: "invalid-address",
		HttpServer: &http.Server{
			Addr:    "invalid-address",
			Handler: http.NewServeMux(),
		},
		Readiness: readiness,
	}

	runtime.Run()

	if scheduler.startCalls != 1 {
		t.Fatalf("scheduler Start calls = %d, want 1", scheduler.startCalls)
	}
	if scheduler.stopCalls != 1 {
		t.Fatalf("scheduler Stop calls = %d, want 1", scheduler.stopCalls)
	}

	statusCode, payload := readiness.Response()
	if statusCode != http.StatusServiceUnavailable {
		t.Fatalf("readiness status code = %d, want %d", statusCode, http.StatusServiceUnavailable)
	}
	status, _ := payload["status"].(string)
	if status != "not_ready" {
		t.Fatalf("readiness status = %q, want %q", status, "not_ready")
	}
	if _, exists := payload["last_error"]; exists {
		t.Fatal("last_error should be hidden from readiness payload")
	}
}

var _ youtube.Service = (*fakeYouTubeService)(nil)
var _ youtube.Scheduler = (*fakeScheduler)(nil)
