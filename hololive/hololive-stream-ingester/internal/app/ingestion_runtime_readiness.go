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

package app

import (
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/kapu/hololive-shared/pkg/health"
)

type ingestionReadinessState struct {
	runtimeName       string
	youtubeEnabled    bool
	photoSyncEnabled  bool
	httpServerStarted atomic.Bool
	shuttingDown      atomic.Bool
	lastError         atomic.Value // string
}

func newIngestionReadinessState(runtimeName string, features ingestionRuntimeFeatures) *ingestionReadinessState {
	state := &ingestionReadinessState{
		runtimeName:      strings.TrimSpace(runtimeName),
		youtubeEnabled:   features.youtubeEnabled,
		photoSyncEnabled: features.photoSyncEnabled,
	}
	state.lastError.Store("")
	return state
}

func runtimeHTTPServerOperationName(runtimeName string) string {
	name := strings.TrimSpace(runtimeName)
	if name == "" {
		name = streamIngesterRuntimeName
	}
	return name + "-http"
}

func (s *ingestionReadinessState) markRunning() {
	if s == nil {
		return
	}
	s.httpServerStarted.Store(true)
	s.shuttingDown.Store(false)
	s.lastError.Store("")
}

func (s *ingestionReadinessState) markStopping(message string) {
	if s == nil {
		return
	}
	s.shuttingDown.Store(true)
	if strings.TrimSpace(message) != "" {
		s.lastError.Store(message)
	}
}

func (s *ingestionReadinessState) response() (int, map[string]any) {
	base := health.Get()
	if s == nil {
		return http.StatusServiceUnavailable, map[string]any{
			"status":     "not_ready",
			"version":    base.Version,
			"uptime":     base.Uptime,
			"goroutines": base.Goroutines,
		}
	}

	ready := s.httpServerStarted.Load() && !s.shuttingDown.Load()
	statusCode := http.StatusOK
	status := "ready"
	if !ready {
		statusCode = http.StatusServiceUnavailable
		status = "not_ready"
	}

	response := map[string]any{
		"status":              status,
		"version":             base.Version,
		"uptime":              base.Uptime,
		"goroutines":          base.Goroutines,
		"runtime":             s.runtimeName,
		"http_server_started": s.httpServerStarted.Load(),
		"shutting_down":       s.shuttingDown.Load(),
		"youtube_enabled":     s.youtubeEnabled,
		"photo_sync_enabled":  s.photoSyncEnabled,
	}
	if lastError, _ := s.lastError.Load().(string); strings.TrimSpace(lastError) != "" {
		response["last_error"] = lastError
	}

	return statusCode, response
}
