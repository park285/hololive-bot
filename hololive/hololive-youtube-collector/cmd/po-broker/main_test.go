package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-youtube-collector/internal/runtime/pobroker"
)

const signalHelperEnv = "PO_BROKER_SIGNAL_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(signalHelperEnv) == "1" {
		runSignalHelper()

		return
	}

	os.Exit(m.Run())
}

func runSignalHelper() {
	watchTermination(exitReporter(os.Stderr, "generation"), func() pobroker.ExitReason { return "" })

	if _, err := os.Stdout.WriteString("ready\n"); err != nil {
		os.Exit(3)
	}

	select {}
}

func TestWatchTerminationRecordsAndExitsBySignal(t *testing.T) {
	const line = "po-broker exit reason=signal generation=generation\n"

	const (
		plain          = `exec "$0"`
		ignoringSIGINT = `trap '' INT; exec "$0"`
	)

	tests := []struct {
		name   string
		script string
		send   []syscall.Signal
		want   syscall.Signal
	}{
		{name: "sigterm", script: plain, send: []syscall.Signal{syscall.SIGTERM}, want: syscall.SIGTERM},
		{name: "sigint", script: plain, send: []syscall.Signal{syscall.SIGINT}, want: syscall.SIGINT},
		{name: "inherited ignored sigint", script: ignoringSIGINT, send: []syscall.Signal{syscall.SIGINT, syscall.SIGTERM}, want: syscall.SIGTERM},
	}

	helper, err := os.Executable()
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.script == plain && slices.Contains(tt.send, syscall.SIGINT) && signal.Ignored(syscall.SIGINT) {
				t.Skip("test process inherited an ignored SIGINT, so the helper cannot receive it")
			}

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			//nolint:gosec // G204/G702: 외부 입력이 없는 테스트 helper 재실행이라 taint 경고는 오탐이다.
			cmd := exec.CommandContext(ctx, "/bin/sh", "-c", tt.script, helper)

			cmd.Env = append(os.Environ(), signalHelperEnv+"=1")

			var stderr bytes.Buffer

			cmd.Stderr = &stderr

			stdout, err := cmd.StdoutPipe()
			require.NoError(t, err)
			require.NoError(t, cmd.Start())

			ready, err := bufio.NewReader(stdout).ReadString('\n')
			require.NoError(t, err)
			require.Equal(t, "ready\n", ready)

			for _, sig := range tt.send {
				require.NoError(t, cmd.Process.Signal(sig))
			}

			var exitErr *exec.ExitError

			require.ErrorAs(t, cmd.Wait(), &exitErr)

			status, ok := exitErr.Sys().(syscall.WaitStatus)
			require.True(t, ok)
			require.True(t, status.Signaled(), "helper exited instead of dying by signal: %v", exitErr)
			require.Equal(t, tt.want, status.Signal())
			require.Equal(t, line, stderr.String())
			require.NotErrorIs(t, ctx.Err(), context.DeadlineExceeded, "helper swallowed the termination signal")
		})
	}
}

func TestStartupFailureReportRedactsUnderlyingErrors(t *testing.T) {
	const privateOutput = "fixture-worker-output"

	tests := []struct {
		name           string
		err            error
		classification string
	}{
		{"deadline", errors.Join(errors.New(privateOutput), context.DeadlineExceeded), "deadline"},
		{"canceled", errors.Join(errors.New(privateOutput), context.Canceled), "canceled"},
		{"unknown", errors.New(privateOutput), "unclassified"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer

			reportStartupFailure(&output, tt.err)

			require.NotContains(t, output.String(), privateOutput)
			require.Contains(t, output.String(), "startup_failure="+tt.classification)
		})
	}
}
