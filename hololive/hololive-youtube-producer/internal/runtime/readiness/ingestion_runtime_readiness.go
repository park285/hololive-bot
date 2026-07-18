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
	"sync"
	"sync/atomic"
	"time"

	"github.com/kapu/hololive-shared/pkg/health"
	sharedreadiness "github.com/kapu/hololive-shared/pkg/readiness"
)

const (
	defaultRuntimeName          = "youtube-producer"
	defaultScraperFetcherEngine = "nethttp"
	scraperFetcherEngineBrowser = "browser_snapshot"
)

type Features struct {
	YouTubeEnabled       bool
	PhotoSyncEnabled     bool
	ActiveActiveEnabled  bool
	ActiveActiveInstance string
	GlobalBudgetEnabled  bool
	ScraperFetcherEngine string
}

type State struct {
	runtimeName             string
	youtubeEnabled          bool
	photoSyncEnabled        bool
	activeActiveEnabled     bool
	globalBudgetEnabled     atomic.Bool
	instanceID              string
	leaseAvailable          atomic.Bool
	budgetBackendAvailable  atomic.Bool
	httpServerStarted       atomic.Bool
	shuttingDown            atomic.Bool
	leaseReason             atomic.Value // string
	lastError               atomic.Value // string
	scraperFetcherEngine    atomic.Value // string
	budgetMu                sync.Mutex
	budgetExhausted         map[string]struct{}
	sourceCooldown          map[string]time.Time
	budgetCleanupIncomplete map[string]struct{}
	nowFunc                 func() time.Time
}

func (s *State) now() time.Time {
	if s.nowFunc != nil {
		return s.nowFunc()
	}
	return time.Now()
}

func New(runtimeName string, features Features) *State {
	state := &State{
		runtimeName:             strings.TrimSpace(runtimeName),
		youtubeEnabled:          features.YouTubeEnabled,
		photoSyncEnabled:        features.PhotoSyncEnabled,
		activeActiveEnabled:     features.ActiveActiveEnabled,
		instanceID:              strings.TrimSpace(features.ActiveActiveInstance),
		budgetExhausted:         make(map[string]struct{}),
		sourceCooldown:          make(map[string]time.Time),
		budgetCleanupIncomplete: make(map[string]struct{}),
	}
	state.leaseReason.Store("")
	state.lastError.Store("")
	state.scraperFetcherEngine.Store(normalizeScraperFetcherEngine(features.ScraperFetcherEngine))
	state.leaseAvailable.Store(true)
	state.globalBudgetEnabled.Store(features.GlobalBudgetEnabled)
	state.budgetBackendAvailable.Store(true)
	if features.ActiveActiveEnabled {
		state.MarkLeaseUnavailable("valkey_unavailable_active_active_fail_closed")
	}
	return state
}

func (s *State) SetScraperFetcherEngine(engine string) {
	if s == nil {
		return
	}
	s.scraperFetcherEngine.Store(normalizeScraperFetcherEngine(engine))
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

func (s *State) Response() (statusCode int, payload map[string]any) {
	base := health.Get()
	if s == nil {
		return http.StatusServiceUnavailable, nilReadinessPayload(base)
	}

	leaseAvailable := s.leaseAvailable.Load()
	leaseEnabled := s.activeActiveEnabled
	budgetEnabled := s.globalBudgetEnabled.Load()
	budgetBackendAvailable := !budgetEnabled || s.budgetBackendAvailable.Load()
	budgetExhausted, sourceCooldown, budgetCleanupIncomplete, affectedSources := s.budgetAdmissionPayload(budgetEnabled)
	statusCode, status := sharedreadiness.HTTPStatus(s.ready(leaseEnabled, leaseAvailable, budgetEnabled, budgetBackendAvailable))
	response := s.responsePayload(base, status, leaseEnabled, leaseAvailable, budgetEnabled, budgetBackendAvailable, budgetExhausted, sourceCooldown, budgetCleanupIncomplete, affectedSources)
	s.addLeaseReason(response, leaseEnabled, leaseAvailable)
	return statusCode, response
}

func (s *State) PublicResponse() (statusCode int, payload map[string]any) {
	base := health.Get()
	if s == nil {
		return http.StatusServiceUnavailable, publicReadinessPayload(base, "not_ready")
	}

	leaseAvailable := s.leaseAvailable.Load()
	leaseEnabled := s.activeActiveEnabled
	budgetEnabled := s.globalBudgetEnabled.Load()
	budgetBackendAvailable := !budgetEnabled || s.budgetBackendAvailable.Load()
	statusCode, status := sharedreadiness.HTTPStatus(s.ready(leaseEnabled, leaseAvailable, budgetEnabled, budgetBackendAvailable))
	return statusCode, publicReadinessPayload(base, status)
}

func publicReadinessPayload(base health.Response, status string) map[string]any {
	return sharedreadiness.BasePayload(base, status)
}

func nilReadinessPayload(base health.Response) map[string]any {
	payload := sharedreadiness.BasePayload(base, "not_ready")
	payload["budget_backend_available"] = true
	payload["budget_exhausted"] = false
	payload["source_cooldown"] = false
	payload["budget_cleanup_incomplete"] = false
	payload["affected_sources"] = []string{}
	payload["scraper_fetcher_engine"] = defaultScraperFetcherEngine
	return payload
}

func (s *State) ready(leaseEnabled, leaseAvailable, budgetEnabled, budgetBackendAvailable bool) bool {
	return s.httpServerStarted.Load() &&
		!s.shuttingDown.Load() &&
		(!leaseEnabled || leaseAvailable) &&
		(!budgetEnabled || budgetBackendAvailable)
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
	budgetCleanupIncomplete bool,
	affectedSources []string,
) map[string]any {
	payload := sharedreadiness.BasePayload(base, status)
	payload["runtime"] = s.runtimeName
	payload["http_server_started"] = s.httpServerStarted.Load()
	payload["shutting_down"] = s.shuttingDown.Load()
	payload["youtube_enabled"] = s.youtubeEnabled
	payload["photo_sync_enabled"] = s.photoSyncEnabled
	payload["mode"] = readinessMode(s.activeActiveEnabled)
	payload["active_active"] = s.activeActiveEnabled
	payload["instance_id"] = s.instanceID
	payload["job_lease_enabled"] = leaseEnabled
	payload["valkey_available"] = !leaseEnabled || leaseAvailable
	payload["budget_backend_available"] = budgetBackendAvailable
	payload["budget_exhausted"] = budgetEnabled && budgetExhausted
	payload["source_cooldown"] = budgetEnabled && sourceCooldown
	payload["budget_cleanup_incomplete"] = budgetEnabled && budgetCleanupIncomplete
	payload["affected_sources"] = affectedSources
	payload["scraper_fetcher_engine"] = s.currentScraperFetcherEngine()
	payload["scraping_paused"] = (leaseEnabled && !leaseAvailable) || (budgetEnabled && !budgetBackendAvailable)
	return payload
}

func (s *State) addLeaseReason(response map[string]any, leaseEnabled, leaseAvailable bool) {
	if leaseEnabled && !leaseAvailable {
		if reason, ok := s.leaseReason.Load().(string); ok && reason != "" {
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

func (s *State) currentScraperFetcherEngine() string {
	if s == nil {
		return defaultScraperFetcherEngine
	}
	engine, ok := s.scraperFetcherEngine.Load().(string)
	if !ok {
		return defaultScraperFetcherEngine
	}
	return normalizeScraperFetcherEngine(engine)
}

func normalizeScraperFetcherEngine(engine string) string {
	switch strings.TrimSpace(engine) {
	case scraperFetcherEngineBrowser:
		return scraperFetcherEngineBrowser
	default:
		return defaultScraperFetcherEngine
	}
}
