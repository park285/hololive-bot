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

// errBeforeDispatch는 취소된 요청이 워커 I/O를 시작하지 않았음을 나타낸다.
var errBeforeDispatch = errors.New("request canceled before worker dispatch")

type worker struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	out   *bufio.Reader
	done  chan struct{}
	// waitErr는 done을 닫기 전에 한 번 쓰고 닫힌 뒤에만 읽는다.
	waitErr error
	frame   []byte
}

func (b *Broker) initializeWorker(ctx context.Context) error {
	worker, err := startWorker(ctx, b.node, b.script)
	if err != nil {
		return err
	}

	b.mu.Lock()

	if b.retiring || ctx.Err() != nil {
		b.mu.Unlock()

		return errors.Join(errWorker, worker.stop())
	}

	b.worker = worker
	b.mu.Unlock()

	var loaded struct {
		Type string `json:"type"`
	}

	if err := worker.exchange(ctx, nil, &loaded); err != nil || loaded.Type != "loaded" {
		return errors.Join(errWorker, err)
	}

	// 기동 중 종료는 exchange가 실패로 돌려주어 Serve가 startup_failed로 퇴역시킵니다.
	// 감시는 loaded 뒤에 시작해 기동 실패의 퇴역 원인을 worker_exited와 경합시키지 않습니다.
	// 요청이 worker IO(직렬 슬롯)를 쥔 중의 종료는 그 요청 경로와 같은 worker_failed로 기록합니다.
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
	// VM 리더가 종료된 뒤 하위 프로세스가 stderr를 보유해도 Wait와 브로커 종료를 막지 못하게 한다.
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

	cmd.Stderr = io.Discard // os/exec가 원본 stderr를 저장·출력하지 않고 비운다.
	if err := cmd.Start(); err != nil {
		_ = in.Close()
		_ = out.Close()

		return nil, errWorker
	}

	w := &worker{cmd: cmd, stdin: in, out: bufio.NewReaderSize(out, 4096), done: make(chan struct{}), frame: make([]byte, 0, maxWorkerFrame)}

	// 정상 여부와 관계없이 done을 닫아 해당 세대를 종료한다.
	go func() {
		w.waitErr = cmd.Wait()
		close(w.done)
	}()

	return w, nil
}

// 파이프 I/O는 요청 제한 시간을 상속하지 않아 전용 goroutine을 사용한다.
// 취소되면 브로커가 프로세스 그룹을 종료한다.
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
		// 성공 응답이 파이프에 남아 있어도 워커 종료를 복구 가능한 전이로 취급하지 않는다.
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

// stop은 VM 프로세스 그룹을 종료하고 제한된 시간 동안 리더 회수를 기다린다.
// 수행·확인하지 못한 정리만 오류로 보고하며 워커 출력은 포함하지 않는다.
func (w *worker) stop() error {
	if w == nil {
		return nil
	}

	var err error

	// 리더가 종료되어도 그룹 구성원이 남을 수 있다. ESRCH여야 모두 사라졌다고 확인할 수 있다.
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

// reapFailure는 종료 코드·신호와 관계없이 리더 회수 여부만 판정한다.
// 파이프를 보유한 하위 프로세스는 그룹 종료로 제거한다.
func reapFailure(err error) error {
	if _, exited := errors.AsType[*exec.ExitError](err); err == nil || exited || errors.Is(err, exec.ErrWaitDelay) {
		return nil
	}

	return fmt.Errorf("reap worker: %w", err)
}
