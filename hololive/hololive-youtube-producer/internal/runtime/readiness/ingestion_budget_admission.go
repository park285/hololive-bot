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
	"sort"
	"strings"
	"time"
)

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
	if !isBudgetAdmissionReason(reason) {
		return
	}
	normalized := normalizeBudgetSources(sources)
	if len(normalized) == 0 {
		return
	}
	s.budgetMu.Lock()
	defer s.budgetMu.Unlock()
	for _, source := range normalized {
		s.applyBudgetAdmissionLocked(reason, source)
	}
}

func isBudgetAdmissionReason(reason string) bool {
	return reason == "budget_exhausted" ||
		reason == "source_cooldown" ||
		reason == "budget_cleanup_incomplete"
}

func (s *State) applyBudgetAdmissionLocked(reason, source string) {
	switch reason {
	case "source_cooldown":
		s.sourceCooldown[source] = time.Time{}
		delete(s.budgetExhausted, source)
		delete(s.budgetCleanupIncomplete, source)
	case "budget_cleanup_incomplete":
		s.budgetCleanupIncomplete[source] = struct{}{}
		delete(s.budgetExhausted, source)
		delete(s.sourceCooldown, source)
	default:
		s.budgetExhausted[source] = struct{}{}
		delete(s.sourceCooldown, source)
		delete(s.budgetCleanupIncomplete, source)
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
		delete(s.budgetCleanupIncomplete, source)
	}
}

func (s *State) budgetAdmissionPayload(budgetEnabled bool) (budgetExhausted, sourceCooldown, budgetCleanupIncomplete bool, affectedSources []string) {
	if !budgetEnabled {
		return false, false, false, []string{}
	}
	s.budgetMu.Lock()
	defer s.budgetMu.Unlock()
	s.pruneExpiredCooldownsLocked()
	affected := make(map[string]struct{}, len(s.budgetExhausted)+len(s.sourceCooldown)+len(s.budgetCleanupIncomplete))
	for source := range s.budgetExhausted {
		affected[source] = struct{}{}
	}
	for source := range s.sourceCooldown {
		affected[source] = struct{}{}
	}
	for source := range s.budgetCleanupIncomplete {
		affected[source] = struct{}{}
	}
	sources := make([]string, 0, len(affected))
	for source := range affected {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return len(s.budgetExhausted) > 0, len(s.sourceCooldown) > 0, len(s.budgetCleanupIncomplete) > 0, sources
}

func (s *State) pruneExpiredCooldownsLocked() {
	now := s.now()
	for source, expiry := range s.sourceCooldown {
		if !expiry.IsZero() && now.After(expiry) {
			delete(s.sourceCooldown, source)
		}
	}
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
