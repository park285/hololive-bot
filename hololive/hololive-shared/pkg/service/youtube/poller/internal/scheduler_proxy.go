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

// Package poller: YouTube 채널 데이터 폴링 및 스케줄링
package polling

import "log/slog"

// 반환값은 토글 적용을 시도한 폴러 수입니다.
func (s *Scheduler) SetProxyEnabled(enabled bool) int {
	pollers := s.collectProxyTogglePollers()
	applied := 0
	for _, poller := range pollers {
		if poller.SetProxyEnabled(enabled) {
			applied++
		}
	}

	slog.Info("Scheduler proxy toggle applied",
		"enabled", enabled,
		"pollers", len(pollers),
		"applied", applied)

	return applied
}

// known=false이면 프록시 토글 지원 폴러가 없음을 의미합니다.
func (s *Scheduler) ProxyEnabled() (enabled bool, known bool) {
	pollers := s.collectProxyTogglePollers()
	if len(pollers) == 0 {
		return false, false
	}
	return pollers[0].ProxyEnabled(), true
}

func (s *Scheduler) collectProxyTogglePollers() []proxyTogglePoller {
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := make(map[Poller]struct{})
	pollers := make([]proxyTogglePoller, 0)
	for _, job := range s.jobMap {
		if job == nil || job.Poller == nil {
			continue
		}
		if _, exists := seen[job.Poller]; exists {
			continue
		}
		seen[job.Poller] = struct{}{}

		toggler, ok := job.Poller.(proxyTogglePoller)
		if !ok {
			continue
		}
		pollers = append(pollers, toggler)
	}
	return pollers
}
