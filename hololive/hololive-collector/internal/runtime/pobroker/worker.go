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

var errBeforeDispatch = errors.New("request canceled before worker dispatch")

var errDeadlineBeforeDispatch = errors.Join(errBeforeDispatch, context.DeadlineExceeded)

type worker struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	out     *bufio.Reader
	done    chan struct{}
	waitErr error
	frame   []byte
}

func (b *Broker) initializeWorker(ctx context.Context) error {
	worker, err := startWorker(ctx, b.node, b.script)
	if err != nil {
		return err
	}

	return b.loadWorker(ctx, worker)
}

func (b *Broker) loadWorker(ctx context.Context, worker *worker) error {
	b.mu.Lock()

	if err := ctx.Err(); b.retiring || err != nil {
		b.mu.Unlock()

		stopErr := worker.stop()

		if errors.Is(err, context.DeadlineExceeded) {
			return errors.Join(errWorker, errDeadlineBeforeDispatch, stopErr)
		}

		return errors.Join(errWorker, stopErr)
	}

	b.worker = worker
	b.mu.Unlock()

	var loaded struct {
		Type string `json:"type"`
	}

	if err := worker.exchange(ctx, nil, &loaded); err != nil || loaded.Type != "loaded" {
		return errors.Join(errWorker, err)
	}

	go func() {
		<-worker.done

		select {
		case b.serial <- struct{}{}:
			b.retire(ExitWorkerExited)
			<-b.serial
		default:
			b.retire(ExitWorkerFailed)
		}
	}()

	return nil
}

func startWorker(ctx context.Context, node, script string) (*worker, error) {
	if err := beforeDispatch(ctx); err != nil {
		return nil, err
	}

	//nolint:contextcheck // generation 전용 수명
	// #nosec G204 -- 경로는 요청 데이터가 아닌 운영 시작 인수이며 rootfs로 제한됩니다.
	cmd := exec.CommandContext(context.Background(), node, "--permission", "--allow-fs-read=/app/po-sandbox",
		"--no-experimental-global-navigator", "--max-old-space-size=256", script)

	cmd.Env = []string{"HOME=/tmp", "TMPDIR=/tmp", "TZ=UTC", "LANG=C.UTF-8"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
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

	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = in.Close()
		_ = out.Close()

		return nil, errWorker
	}

	w := &worker{cmd: cmd, stdin: in, out: bufio.NewReaderSize(out, 4096), done: make(chan struct{}), frame: make([]byte, 0, maxWorkerFrame)}

	go func() {
		w.waitErr = cmd.Wait()
		close(w.done)
	}()

	return w, nil
}

func beforeDispatch(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return errDeadlineBeforeDispatch
		}

		return errBeforeDispatch
	}

	return nil
}

func (w *worker) exchange(ctx context.Context, input, output any) error {
	if err := beforeDispatch(ctx); err != nil {
		return err
	}

	var data []byte

	if input != nil {
		var err error

		data, err = json.Marshal(input)
		if err != nil {
			return errWorker
		}

		data = append(data, '\n')
	}

	if err := beforeDispatch(ctx); err != nil {
		return err
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

func (w *worker) stop() error {
	if w == nil {
		return nil
	}

	var err error

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

func reapFailure(err error) error {
	if _, exited := errors.AsType[*exec.ExitError](err); err == nil || exited || errors.Is(err, exec.ErrWaitDelay) {
		return nil
	}

	return fmt.Errorf("reap worker: %w", err)
}
