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
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kapu/hololive-shared/pkg/health"
)

const defaultRuntimeName = "youtube-producer"

type Features struct {
	YouTubeEnabled       bool
	PhotoSyncEnabled     bool
	ActiveActiveEnabled  bool
	ActiveActiveInstance string
	GlobalBudgetEnabled  bool
}

type State struct {
	runtimeName            string
	youtubeEnabled         bool
	photoSyncEnabled       bool
	activeActiveEnabled    bool
	globalBudgetEnabled    atomic.Bool
	instanceID             string
	leaseAvailable         atomic.Bool
	budgetBackendAvailable atomic.Bool
	httpServerStarted      atomic.Bool
	shuttingDown           atomic.Bool
	leaseReason            atomic.Value // string
	lastError              atomic.Value // string
	budgetMu               sync.Mutex
	budgetExhausted        map[string]struct{}
	sourceCooldown         map[string]time.Time
	nowFunc                func() time.Time
}

func (s *State) now() time.Time {
	if s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now()
}

func New(runtimeName string, features Features) *State {
	state := &State{
		runtimeName:         strings.TrimSpace(runtimeName),
		youtubeEnabled:      features.YouTubeEnabled,
		photoSyncEnabled:    features.PhotoSyncEnabled,
		activeActiveEnabled: features.ActiveActiveEnabled,
		instanceID:          strings.TrimSpace(features.ActiveActiveInstance),
		budgetExhausted:     make(map[string]struct{}),
		sourceCooldown:      make(map[string]time.Time),
	}
	state.leaseReason.Store("")
	state.lastError.Store("")
	state.leaseAvailable.Store(true)
	state.globalBudgetEnabled.Store(features.GlobalBudgetEnabled)
	state.budgetBackendAvailable.Store(true)
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

func (s *State) SetGlobalBudgetEnabled(enabled bool) {
	if s == nil {
		return
	}
	s.globalBudgetEnabled.Store(enabled)
}

func (s *State) MarkBudgetBackendUnavailable(reason string) {
	if s == nil {
		return
	}
	s.budgetBackendAvailable.Store(false)
	if reason = strings.TrimSpace(reason); reason != "" {
		s.lastError.Store(reason)
	}
}

func (s *State) MarkBudgetBackendAvailable() {
	if s == nil {
		return
	}
	s.budgetBackendAvailable.Store(true)
}

func (s *State) MarkBudgetAdmissionDenied(reason string, sources []string) {
	if s == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason != "budget_exhausted" && reason != "source_cooldown" {
		return
	}
	normalized := normalizeBudgetSources(sources)
	if len(normalized) == 0 {
		return
	}
	s.budgetMu.Lock()
	defer s.budgetMu.Unlock()
	if reason == "source_cooldown" {
		for _, source := range normalized {
			s.sourceCooldown[source] = time.Time{}
		}
		return
	}
	for _, source := range normalized {
		s.budgetExhausted[source] = struct{}{}
	}
}

// reserve가 다시 돌지 않는 source(fallback 전용 등)도 TTL이 지나면 readiness에서 자동 해제한다.
func (s *State) MarkSourceCooldownFor(sources []string, ttl time.Duration) {
	if s == nil || ttl <= 0 {
		return
	}
	normalized := normalizeBudgetSources(sources)
	if len(normalized) == 0 {
		return
	}
	expiry := s.now().Add(ttl)
	s.budgetMu.Lock()
	defer s.budgetMu.Unlock()
	for _, source := range normalized {
		s.sourceCooldown[source] = expiry
	}
}

func (s *State) ClearBudgetAdmission(sources []string) {
	if s == nil {
		return
	}
	normalized := normalizeBudgetSources(sources)
	if len(normalized) == 0 {
		return
	}
	s.budgetMu.Lock()
	defer s.budgetMu.Unlock()
	for _, source := range normalized {
		delete(s.budgetExhausted, source)
		delete(s.sourceCooldown, source)
	}
}

func (s *State) Response() (int, map[string]any) {
	base := health.Get()
	if s == nil {
		return http.StatusServiceUnavailable, nilReadinessPayload(base)
	}

	leaseAvailable := s.leaseAvailable.Load()
	leaseEnabled := s.activeActiveEnabled
	budgetEnabled := s.globalBudgetEnabled.Load()
	budgetBackendAvailable := !budgetEnabled || s.budgetBackendAvailable.Load()
	budgetExhausted, sourceCooldown, affectedSources := s.budgetAdmissionPayload(budgetEnabled)
	statusCode, status := readinessHTTPStatus(s.ready(leaseEnabled, leaseAvailable, budgetEnabled, budgetBackendAvailable))
	response := s.responsePayload(base, status, leaseEnabled, leaseAvailable, budgetEnabled, budgetBackendAvailable, budgetExhausted, sourceCooldown, affectedSources)
	s.addLeaseReason(response, leaseEnabled, leaseAvailable)
	return statusCode, response
}

func nilReadinessPayload(base health.Response) map[string]any {
	return map[string]any{
		"status":                   "not_ready",
		"version":                  base.Version,
		"uptime":                   base.Uptime,
		"goroutines":               base.Goroutines,
		"budget_backend_available": true,
		"budget_exhausted":         false,
		"source_cooldown":          false,
		"affected_sources":         []string{},
	}
}

func (s *State) ready(leaseEnabled bool, leaseAvailable bool, budgetEnabled bool, budgetBackendAvailable bool) bool {
	return s.httpServerStarted.Load() &&
		!s.shuttingDown.Load() &&
		(!leaseEnabled || leaseAvailable) &&
		(!budgetEnabled || budgetBackendAvailable)
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
	budgetEnabled bool,
	budgetBackendAvailable bool,
	budgetExhausted bool,
	sourceCooldown bool,
	affectedSources []string,
) map[string]any {
	return map[string]any{
		"status":                   status,
		"version":                  base.Version,
		"uptime":                   base.Uptime,
		"goroutines":               base.Goroutines,
		"runtime":                  s.runtimeName,
		"http_server_started":      s.httpServerStarted.Load(),
		"shutting_down":            s.shuttingDown.Load(),
		"youtube_enabled":          s.youtubeEnabled,
		"photo_sync_enabled":       s.photoSyncEnabled,
		"mode":                     readinessMode(s.activeActiveEnabled),
		"active_active":            s.activeActiveEnabled,
		"instance_id":              s.instanceID,
		"job_lease_enabled":        leaseEnabled,
		"valkey_available":         !leaseEnabled || leaseAvailable,
		"budget_backend_available": budgetBackendAvailable,
		"budget_exhausted":         budgetEnabled && budgetExhausted,
		"source_cooldown":          budgetEnabled && sourceCooldown,
		"affected_sources":         affectedSources,
		"scraping_paused":          (leaseEnabled && !leaseAvailable) || (budgetEnabled && !budgetBackendAvailable),
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

func (s *State) budgetAdmissionPayload(budgetEnabled bool) (bool, bool, []string) {
	if !budgetEnabled {
		return false, false, []string{}
	}
	s.budgetMu.Lock()
	defer s.budgetMu.Unlock()
	now := s.now()
	for source, expiry := range s.sourceCooldown {
		if !expiry.IsZero() && now.After(expiry) {
			delete(s.sourceCooldown, source)
		}
	}
	affected := make(map[string]struct{}, len(s.budgetExhausted)+len(s.sourceCooldown))
	for source := range s.budgetExhausted {
		affected[source] = struct{}{}
	}
	for source := range s.sourceCooldown {
		affected[source] = struct{}{}
	}
	sources := make([]string, 0, len(affected))
	for source := range affected {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return len(s.budgetExhausted) > 0, len(s.sourceCooldown) > 0, sources
}

func normalizeBudgetSources(sources []string) []string {
	seen := make(map[string]struct{}, len(sources))
	normalized := make([]string, 0, len(sources))
	for _, source := range sources {
		source = strings.TrimSpace(source)
		if source == "" {
			continue
		}
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		normalized = append(normalized, source)
	}
	return normalized
}
