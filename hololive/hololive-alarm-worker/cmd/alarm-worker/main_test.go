package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRunWorkerProfileCheck(t *testing.T) {
	var stderr bytes.Buffer

	handled, code := runWorkerProfileCheck([]string{"--check-worker-profile"}, &stderr, func() error { return nil })

	if !handled || code != 0 || !strings.Contains(stderr.String(), "worker profile valid") {
		t.Fatalf("runWorkerProfileCheck() = (%t,%d,%q)", handled, code, stderr.String())
	}

	stderr.Reset()

	handled, code = runWorkerProfileCheck([]string{"--check-worker-profile"}, &stderr, func() error { return errors.New("invalid profile") })
	if !handled || code != 1 || !strings.Contains(stderr.String(), "invalid profile") {
		t.Fatalf("runWorkerProfileCheck() failure = (%t,%d,%q)", handled, code, stderr.String())
	}
}
