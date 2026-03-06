package settings

import "testing"

type stubHolodexProxyRuntimeService struct {
	enabled  bool
	setCalls int
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
