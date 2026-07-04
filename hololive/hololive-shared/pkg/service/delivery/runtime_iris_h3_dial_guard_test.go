package delivery

import (
	"net"
	"testing"
)

func TestRuntimeIrisH3DialGuardAllowsOnlyBaseURLLiteralIP(t *testing.T) {
	t.Parallel()

	guard := newRuntimeIrisH3DialGuard(func() (string, error) {
		return "https://192.0.2.10:3001", nil
	})
	if err := guard(net.ParseIP("192.0.2.10")); err != nil {
		t.Fatalf("guard(allowed ip) error = %v", err)
	}
	if err := guard(net.ParseIP("192.0.2.11")); err == nil {
		t.Fatal("guard(disallowed ip) error = nil, want denial")
	}
	if err := guard(nil); err == nil {
		t.Fatal("guard(nil) error = nil, want denial")
	}
}

func TestRuntimeIrisH3DialGuardUsesCurrentResolvedBaseURL(t *testing.T) {
	t.Parallel()

	currentBaseURL := "https://192.0.2.10:3001"
	guard := newRuntimeIrisH3DialGuard(func() (string, error) {
		return currentBaseURL, nil
	})
	if err := guard(net.ParseIP("192.0.2.10")); err != nil {
		t.Fatalf("guard(initial ip) error = %v", err)
	}

	currentBaseURL = "https://192.0.2.11:3001"
	if err := guard(net.ParseIP("192.0.2.11")); err != nil {
		t.Fatalf("guard(updated ip) error = %v", err)
	}
	if err := guard(net.ParseIP("192.0.2.10")); err == nil {
		t.Fatal("guard(old ip) error = nil, want denial after base url update")
	}
}
