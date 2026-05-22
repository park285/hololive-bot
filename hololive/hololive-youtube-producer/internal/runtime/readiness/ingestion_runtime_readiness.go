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

package readiness

import (
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/kapu/hololive-shared/pkg/health"
)

const defaultRuntimeName = "youtube-producer"

type Features struct {
	YouTubeEnabled       bool
	PhotoSyncEnabled     bool
	ActiveActiveEnabled  bool
	ActiveActiveInstance string
}

type State struct {
	runtimeName       string
	youtubeEnabled    bool
	photoSyncEnabled  bool
	activeActiveEnabled      bool
	instanceID        string
	leaseAvailable    atomic.Bool
	httpServerStarted atomic.Bool
	shuttingDown      atomic.Bool
	leaseReason       atomic.Value // string
	lastError         atomic.Value // string
}

func New(runtimeName string, features Features) *State {
	state := &State{
		runtimeName:      strings.TrimSpace(runtimeName),
		youtubeEnabled:   features.YouTubeEnabled,
		photoSyncEnabled: features.PhotoSyncEnabled,
		activeActiveEnabled:     features.ActiveActiveEnabled,
		instanceID:       strings.TrimSpace(features.ActiveActiveInstance),
	}
	state.leaseReason.Store("")
	state.lastError.Store("")
	state.leaseAvailable.Store(true)
	if features.ActiveActiveEnabled {
		state.MarkLeaseUnavailable("valkey_unavailable_active_active_fail_closed")
	}
	return state
}

func HTTPServerOperationName(runtimeName string) string {
	name := strings.TrimSpace(runtimeName)
	if name == "" {
		name = defaultRuntimeName
	}
	return name + "-http"
}

func (s *State) MarkRunning() {
	if s == nil {
		return
	}
	s.httpServerStarted.Store(true)
	s.shuttingDown.Store(false)
	s.lastError.Store("")
}

func (s *State) MarkStopping(message string) {
	if s == nil {
		return
	}
	s.shuttingDown.Store(true)
	if strings.TrimSpace(message) != "" {
		s.lastError.Store(message)
	}
}

func (s *State) MarkLeaseAvailable() {
	if s == nil {
		return
	}
	s.leaseAvailable.Store(true)
	s.leaseReason.Store("")
}

func (s *State) LeaseAvailable() bool {
	if s == nil {
		return false
	}
	return s.leaseAvailable.Load()
}

func (s *State) MarkLeaseUnavailable(reason string) {
	if s == nil {
		return
	}
	s.leaseAvailable.Store(false)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "valkey_unavailable_active_active_fail_closed"
	}
	s.leaseReason.Store(reason)
}

func (s *State) Response() (int, map[string]any) {
	base := health.Get()
	if s == nil {
		return http.StatusServiceUnavailable, nilReadinessPayload(base)
	}

	leaseAvailable := s.leaseAvailable.Load()
	leaseEnabled := s.activeActiveEnabled
	statusCode, status := readinessHTTPStatus(s.ready(leaseEnabled, leaseAvailable))
	response := s.responsePayload(base, status, leaseEnabled, leaseAvailable)
	s.addLeaseReason(response, leaseEnabled, leaseAvailable)
	return statusCode, response
}

func nilReadinessPayload(base health.Response) map[string]any {
	return map[string]any{
		"status":     "not_ready",
		"version":    base.Version,
		"uptime":     base.Uptime,
		"goroutines": base.Goroutines,
	}
}

func (s *State) ready(leaseEnabled bool, leaseAvailable bool) bool {
	return s.httpServerStarted.Load() && !s.shuttingDown.Load() && (!leaseEnabled || leaseAvailable)
}

func readinessHTTPStatus(ready bool) (int, string) {
	if ready {
		return http.StatusOK, "ready"
	}
	return http.StatusServiceUnavailable, "not_ready"
}

func (s *State) responsePayload(
	base health.Response,
	status string,
	leaseEnabled bool,
	leaseAvailable bool,
) map[string]any {
	return map[string]any{
		"status":              status,
		"version":             base.Version,
		"uptime":              base.Uptime,
		"goroutines":          base.Goroutines,
		"runtime":             s.runtimeName,
		"http_server_started": s.httpServerStarted.Load(),
		"shutting_down":       s.shuttingDown.Load(),
		"youtube_enabled":     s.youtubeEnabled,
		"photo_sync_enabled":  s.photoSyncEnabled,
		"mode":                readinessMode(s.activeActiveEnabled),
		"active_active":       s.activeActiveEnabled,
		"instance_id":         s.instanceID,
		"job_lease_enabled":   leaseEnabled,
		"valkey_available":    !leaseEnabled || leaseAvailable,
		"scraping_paused":     leaseEnabled && !leaseAvailable,
	}
}

func (s *State) addLeaseReason(response map[string]any, leaseEnabled bool, leaseAvailable bool) {
	if leaseEnabled && !leaseAvailable {
		if reason, _ := s.leaseReason.Load().(string); reason != "" {
			response["reason"] = reason
		}
	}
}

func readinessMode(activeActiveEnabled bool) string {
	if activeActiveEnabled {
		return "active-active"
	}
	return "single-owner"
}
