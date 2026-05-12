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

func TestAutomaxprocsDecision(t *testing.T) {
	tests := []struct {
		name       string
		disable    string
		force      string
		wantRun    bool
		wantReason automaxprocsDecisionReason
	}{
		{name: "native default", wantRun: false, wantReason: automaxprocsDecisionNativeRuntime},
		{name: "forced", force: "1", wantRun: true, wantReason: automaxprocsDecisionForced},
		{name: "disabled overrides forced", disable: "1", force: "1", wantRun: false, wantReason: automaxprocsDecisionDisabled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(DisableEnv, tt.disable)
			t.Setenv(ForceEnv, tt.force)

			got := decideAutomaxprocs()
			if got.run != tt.wantRun || got.reason != tt.wantReason {
				t.Fatalf("decideAutomaxprocs() = {run:%t reason:%v}, want {run:%t reason:%v}", got.run, got.reason, tt.wantRun, tt.wantReason)
			}
		})
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
