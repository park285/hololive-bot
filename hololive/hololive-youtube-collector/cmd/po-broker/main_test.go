package main

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"os/signal"
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

// runSignalHelper는 main과 같은 signal 감시만 설치하고 준비를 알린 뒤 signal을 기다립니다.
// 감시가 signal을 삼키면 테스트 시간 안에 끝나지 않고, 제한 시간 뒤 SIGKILL을 받습니다.
func runSignalHelper() {
	watchTermination(exitReporter(os.Stderr, "generation"), func() pobroker.ExitReason { return "" })

	if _, err := os.Stdout.WriteString("ready\n"); err != nil {
		os.Exit(3)
	}

	time.Sleep(time.Minute)
	os.Exit(3)
}

func TestWatchTerminationRecordsAndExitsBySignal(t *testing.T) {
	const line = "po-broker exit reason=signal generation=generation\n"

	// shell은 helper를 exec해 같은 PID로 바꾸므로 signal은 helper가 직접 받습니다.
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
		// 비대화형 shell의 background 실행처럼 SIGINT를 무시한 채 시작하면 SIGINT는 계속 무시되고,
		// 이어진 SIGTERM은 원인 줄 하나를 남기고 SIGTERM으로 종료해야 합니다.
		{name: "inherited ignored sigint", script: ignoringSIGINT, send: []syscall.Signal{syscall.SIGINT, syscall.SIGTERM}, want: syscall.SIGTERM},
	}

	helper, err := os.Executable()
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.script == plain && signal.Ignored(syscall.SIGINT) {
				t.Skip("test process inherited an ignored SIGINT, so the helper cannot receive it")
			}

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()

			// 인자는 고정 script와 실행 중인 테스트 binary 경로뿐입니다.
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
