package main

import (
	"os"
	"testing"

	"github.com/park285/shared-go/v2/pkg/healthprobe"
)

func TestClearInternalTLSOverrides(t *testing.T) {
	t.Setenv(healthprobe.CACertFileEnv, "/run/internal-ca.pem")
	t.Setenv(healthprobe.ServerNameEnv, "127.0.0.1")

	if err := clearInternalTLSOverrides(); err != nil {
		t.Fatalf("clearInternalTLSOverrides() error = %v", err)
	}

	if value := os.Getenv(healthprobe.CACertFileEnv); value != "" {
		t.Fatalf("%s = %q", healthprobe.CACertFileEnv, value)
	}

	if value := os.Getenv(healthprobe.ServerNameEnv); value != "" {
		t.Fatalf("%s = %q", healthprobe.ServerNameEnv, value)
	}

	if externalSmokeURL != "https://www.google.com/generate_204" {
		t.Fatalf("externalSmokeURL = %q", externalSmokeURL)
	}
}
