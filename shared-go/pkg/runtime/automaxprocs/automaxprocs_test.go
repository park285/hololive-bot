package automaxprocs

import "testing"

func TestShouldRunAutomaxprocsUsesNativeRuntimeByDefault(t *testing.T) {
	t.Setenv(DisableEnv, "")
	t.Setenv(ForceEnv, "")

	if shouldRunAutomaxprocs() {
		t.Fatal("shouldRunAutomaxprocs() = true, want false without AUTOMAXPROCS_FORCE")
	}
}

func TestShouldRunAutomaxprocsHonorsForceEnv(t *testing.T) {
	t.Setenv(DisableEnv, "")
	t.Setenv(ForceEnv, "1")

	if !shouldRunAutomaxprocs() {
		t.Fatal("shouldRunAutomaxprocs() = false, want true with AUTOMAXPROCS_FORCE=1")
	}
}

func TestShouldRunAutomaxprocsDisableOverridesForce(t *testing.T) {
	t.Setenv(DisableEnv, "1")
	t.Setenv(ForceEnv, "1")

	if shouldRunAutomaxprocs() {
		t.Fatal("shouldRunAutomaxprocs() = true, want false when AUTOMAXPROCS_DISABLE=1")
	}
}

func TestIsTruthy(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{value: "1", want: true},
		{value: "true", want: true},
		{value: "YES", want: true},
		{value: "y", want: true},
		{value: "", want: false},
		{value: "0", want: false},
		{value: "false", want: false},
	}

	for _, tt := range tests {
		if got := isTruthy(tt.value); got != tt.want {
			t.Fatalf("isTruthy(%q) = %t, want %t", tt.value, got, tt.want)
		}
	}
}
