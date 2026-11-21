package entrypoint

import (
	"bytes"
	"errors"
	"testing"
)

func TestRunWorkerProfileCheckWritesRuntimeNamedResult(t *testing.T) {
	var stderr bytes.Buffer

	handled, code := RunWorkerProfileCheck([]string{"--check-worker-profile"}, &stderr, "collector", func() error { return nil })
	if !handled || code != 0 || stderr.String() != "collector worker profile valid\n" {
		t.Fatalf("RunWorkerProfileCheck() = (%t,%d,%q)", handled, code, stderr.String())
	}

	stderr.Reset()

	handled, code = RunWorkerProfileCheck([]string{"--check-worker-profile"}, &stderr, "collector", func() error { return errors.New("invalid profile") })
	if !handled || code != 1 || stderr.String() != "Failed to load collector worker profile: invalid profile\n" {
		t.Fatalf("RunWorkerProfileCheck() failure = (%t,%d,%q)", handled, code, stderr.String())
	}
}

func TestRunWorkerProfileCheckIgnoresOtherArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"--startup-timeout=30s"}, {"--check-worker-profile", "--wait-for-database"}} {
		called := false

		handled, code := RunWorkerProfileCheck(args, &bytes.Buffer{}, "alarm-worker", func() error {
			called = true
			return nil
		})
		if handled || code != 0 || called {
			t.Fatalf("RunWorkerProfileCheck(%q) = (%t,%d) called=%t, want ordinary startup", args, handled, code, called)
		}
	}
}
