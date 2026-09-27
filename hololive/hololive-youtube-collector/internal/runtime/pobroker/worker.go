package pobroker

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const maxWorkerFrame = 64 << 10

var errWorker = errors.New("worker failed")

// errBeforeDispatch indicates that a canceled request never entered worker IO.
var errBeforeDispatch = errors.New("request canceled before worker dispatch")

type worker struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   *bufio.Reader
	done  chan struct{}
	// waitErr is written once before done closes and read only after it.
	waitErr error
	frame   []byte
}

// worker는 generation이 소유합니다. 시작 전 취소는 거부하되, 장수 VM에
// 요청 context의 값·참조를 보존하지 않습니다. 종료는 stop/retirement가 담당합니다.
func startWorker(ctx context.Context, node, script string) (*worker, error) {
	if ctx.Err() != nil {
		return nil, errBeforeDispatch
	}

	// contextcheck: generation은 시작 RPC보다 오래 살며 stop이 종료하므로 요청 값도 보존하지 않습니다.
	//nolint:contextcheck,gosec // generation 전용 수명; G204: 경로는 요청 데이터가 아닌 운영 시작 인수이며 rootfs로 제한됩니다.
	cmd := exec.CommandContext(context.Background(), node, "--permission", "--allow-fs-read=/app/po-sandbox",
		"--no-experimental-global-navigator", "--max-old-space-size=256", script)

	cmd.Env = []string{"HOME=/tmp", "TMPDIR=/tmp", "TZ=UTC", "LANG=C.UTF-8"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// A descendant retaining stderr cannot keep Wait (and broker retirement)
	// blocked after the VM leader exits.
	cmd.WaitDelay = 500 * time.Millisecond

	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, errWorker
	}

	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return nil, errWorker
	}

	cmd.Stderr = io.Discard // os/exec drains without preserving or emitting raw stderr.
	if err := cmd.Start(); err != nil {
		_ = in.Close()
		_ = out.Close()

		return nil, errWorker
	}

	w := &worker{cmd: cmd, stdin: in, out: bufio.NewReaderSize(out, 4096), done: make(chan struct{}), frame: make([]byte, 0, maxWorkerFrame)}

	// Any exit, clean or not, closes done and thereby retires the generation.
	go func() {
		w.waitErr = cmd.Wait()
		close(w.done)
	}()

	return w, nil
}

// A dedicated IO goroutine is necessary because pipe reads and writes do not
// inherit request deadlines. On cancellation the broker kills the process group.
func (w *worker) exchange(ctx context.Context, input, output any) error {
	if ctx.Err() != nil {
		return errBeforeDispatch
	}

	// nil input은 최초 loaded frame 수신 전용이며 worker에 명령을 쓰지 않는다.
	var data []byte

	if input != nil {
		var err error

		data, err = json.Marshal(input)
		if err != nil || len(data)+1 > 1<<20 {
			return errWorker
		}

		data = append(data, '\n')
	}

	if ctx.Err() != nil {
		return errBeforeDispatch
	}

	result := make(chan error, 1)

	go func() { result <- w.exchangeFrame(data, output) }()

	select {
	case err := <-result:
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		// A successful response may already be buffered in the pipe. Nonetheless
		// worker exit is never a recoverable phase transition.
		return errWorker
	}
}

func (w *worker) exchangeFrame(data []byte, output any) error {
	if len(data) > 0 {
		if _, err := w.stdin.Write(data); err != nil {
			return errWorker
		}
	}

	line, err := w.readFrame()
	if err != nil || len(line) < 2 || line[len(line)-1] != '\n' ||
		json.Unmarshal(line[:len(line)-1], output, json.RejectUnknownMembers(true)) != nil {
		return errWorker
	}

	return nil
}

func (w *worker) readFrame() ([]byte, error) {
	line, err := w.out.ReadSlice('\n')
	if err == nil {
		return line, nil
	}

	if !errors.Is(err, bufio.ErrBufferFull) {
		return nil, errWorker
	}

	buffer := w.frame[:0]

	for {
		if len(buffer)+len(line) > maxWorkerFrame {
			return nil, errWorker
		}

		buffer = append(buffer, line...)

		if err == nil {
			return buffer, nil
		}

		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, errWorker
		}

		line, err = w.out.ReadSlice('\n')
	}
}

// stop kills the VM process group with a bounded wait for the leader. The
// error reports only cleanup the broker could not perform or confirm; it never
// carries worker output.
func (w *worker) stop() error {
	if w == nil {
		return nil
	}

	var err error

	// Even if the leader exited, surviving members retain the process group.
	// ESRCH proves no member remains.
	if killErr := syscall.Kill(-w.cmd.Process.Pid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
		err = fmt.Errorf("kill worker process group: %w", killErr)
	}

	_ = w.stdin.Close()

	select {
	case <-w.done:
		return errors.Join(err, reapFailure(w.waitErr))
	case <-time.After(500 * time.Millisecond):
	}

	if killErr := w.cmd.Process.Kill(); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
		return errors.Join(err, fmt.Errorf("kill worker leader: %w", killErr))
	}

	return err
}

// reapFailure ignores every ordinary exit: exit statuses, signals and a pipe
// holder outliving the leader (the group kill removes it) all mean the leader
// was reaped. Only a failure to reap the leader is a cleanup failure.
func reapFailure(err error) error {
	if _, exited := errors.AsType[*exec.ExitError](err); err == nil || exited || errors.Is(err, exec.ErrWaitDelay) {
		return nil
	}

	return fmt.Errorf("reap worker: %w", err)
}
