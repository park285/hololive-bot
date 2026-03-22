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

package settings

import "testing"

type stubHolodexProxyRuntimeService struct {
	enabled  bool
	setCalls int
}

type nilUnsafeProxyToggler struct{}

func (t *nilUnsafeProxyToggler) SetProxyEnabled(bool) int {
	return 0
}

func (t *nilUnsafeProxyToggler) ProxyEnabled() (bool, bool) {
	_ = *t
	return false, false
}

func (s *stubHolodexProxyRuntimeService) SetScraperProxyEnabled(enabled bool) bool {
	s.setCalls++
	s.enabled = enabled
	return true
}

func (s *stubHolodexProxyRuntimeService) ScraperProxyEnabled() bool {
	return s.enabled
}

func TestLocalSettingsApplier_ApplyScraperProxy_HolodexOnly(t *testing.T) {
	t.Parallel()

	holodexSvc := &stubHolodexProxyRuntimeService{}
	applier := NewLocalSettingsApplier(nil, holodexSvc, nil, nil)

	result := applier.ApplyScraperProxy(t.Context(), true)
	if result.HolodexApplied == nil || !*result.HolodexApplied {
		t.Fatalf("HolodexApplied = %#v, want true", result.HolodexApplied)
	}
	if result.HolodexEnabled == nil || !*result.HolodexEnabled {
		t.Fatalf("HolodexEnabled = %#v, want true", result.HolodexEnabled)
	}
	if holodexSvc.setCalls != 1 {
		t.Fatalf("setCalls = %d, want 1", holodexSvc.setCalls)
	}

	runtime := applier.ScraperProxyRuntimeState(true)
	if runtime.HolodexEnabled == nil || !*runtime.HolodexEnabled {
		t.Fatalf("runtime.HolodexEnabled = %#v, want true", runtime.HolodexEnabled)
	}
}

func TestLocalSettingsApplier_ScraperProxyRuntimeState_TypedNilSchedulerDoesNotPanic(t *testing.T) {
	t.Parallel()

	var toggler *nilUnsafeProxyToggler
	applier := NewLocalSettingsApplier(nil, nil, toggler, nil)

	runtime := applier.ScraperProxyRuntimeState(true)

	if runtime.SchedulerEnabled != nil {
		t.Fatalf("runtime.SchedulerEnabled = %#v, want nil", runtime.SchedulerEnabled)
	}
	if runtime.SchedulerKnown != nil {
		t.Fatalf("runtime.SchedulerKnown = %#v, want nil", runtime.SchedulerKnown)
	}
}
