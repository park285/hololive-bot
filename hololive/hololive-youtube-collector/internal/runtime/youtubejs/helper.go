package youtubejs

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kapu/hololive-shared/pkg/panicguard"
)

var (
	ErrUnsupportedHelperPlatform = errors.New("youtube.js helper requires linux")
	ErrCleanupTimedOut           = errors.New("CLEANUP_TIMED_OUT")
)

type Helper struct {
	cmd             *exec.Cmd
	runtimeDir      string
	socketPath      string
	waited          chan struct{}
	waitErr         error
	closeOnce       sync.Once
	closeErr        error
	shutdownTimeout time.Duration
	healthTimeout   time.Duration
	maxInflight     int
	rpc             *RPC
	health          *http.Client
	endpoint        string
	bootstrap       BootstrapResponse
	forcedKills     atomic.Int32
}

func Start(ctx context.Context, config *Config) (*Helper, *RPC, error) {
	if err := requireHelperPlatform(); err != nil {
		return nil, nil, err
	}
	cfg, err := resolveConfig(config)
	if err != nil {
		return nil, nil, err
	}
	runtimeDir, socketPath, err := createRuntimeDir(cfg.RuntimeBaseDir)
	if err != nil {
		return nil, nil, err
	}
	helper := newHelper(runtimeDir, socketPath, &cfg)
	if err := helper.spawn(&cfg); err != nil {
		return nil, nil, failStart(ctx, helper, err)
	}
	startCtx, cancel := withOptionalTimeout(ctx, cfg.StartupTimeout)
	defer cancel()
	if err := helper.waitForSocket(startCtx); err != nil {
		return nil, nil, failStart(ctx, helper, err)
	}
	if err := verifyHelperSocket(socketPath); err != nil {
		return nil, nil, failStart(ctx, helper, err)
	}
	rpc := helper.attachRPC(&cfg)
	if err := helper.bootstrapReady(startCtx, &cfg); err != nil {
		return nil, nil, failStart(ctx, helper, err)
	}
	if err := helper.Healthy(startCtx); err != nil {
		return nil, nil, failStart(ctx, helper, err)
	}
	return helper, rpc, nil
}

func newHelper(runtimeDir, socketPath string, cfg *Config) *Helper {
	return &Helper{
		runtimeDir:      runtimeDir,
		socketPath:      socketPath,
		waited:          make(chan struct{}),
		shutdownTimeout: cfg.ShutdownTimeout,
		healthTimeout:   cfg.HealthTimeout,
		maxInflight:     cfg.MaxInflight,
	}
}

func failStart(ctx context.Context, helper *Helper, err error) error {
	return errors.Join(err, helper.Close(ctx))
}

func (h *Helper) Done() <-chan struct{} {
	if h == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return h.waited
}

func (h *Helper) ProtocolVersion() int {
	if h == nil {
		return 0
	}
	if h.bootstrap.ProtocolVersion != 0 {
		return int(h.bootstrap.ProtocolVersion)
	}
	return int(ProtocolVersion)
}

func (h *Helper) Exited() bool {
	if h == nil || h.waited == nil {
		return true
	}
	select {
	case <-h.waited:
		return true
	default:
		return false
	}
}

func (h *Helper) ExitError() error {
	if h == nil || !h.Exited() {
		return nil
	}
	return h.waitErr
}

func (h *Helper) ForcedKillCount() int {
	if h == nil {
		return 0
	}
	return int(h.forcedKills.Load())
}

func (h *Helper) spawn(cfg *Config) error {
	timeoutMS := cfg.RequestTimeout.Milliseconds()
	if timeoutMS <= 0 {
		timeoutMS = defaultHelperTimeout.Milliseconds()
	}
	args := make([]string, 0, 10+len(cfg.extraArgs))
	args = append(args,
		cfg.NodePath,
		cfg.ScriptPath,
		"--socket", h.socketPath,
		"--protocol-version", strconv.Itoa(int(ProtocolVersion)),
		"--request-read-timeout-ms", strconv.FormatInt(timeoutMS, 10),
		"--shutdown-timeout-ms", strconv.FormatInt(cfg.ShutdownTimeout.Milliseconds(), 10),
	)
	args = append(args, cfg.extraArgs...)
	cmd := &exec.Cmd{
		Path:   cfg.NodePath,
		Args:   args,
		Stdout: os.Stderr,
		Stderr: os.Stderr,
		Env:    helperProcessEnv(os.Environ()),
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start youtube.js helper: %w", err)
	}
	h.cmd = cmd
	panicguard.Go(nil, "youtubejs-helper-wait", func() {
		defer close(h.waited)
		h.waitErr = cmd.Wait()
	})
	return nil
}

func (h *Helper) attachRPC(cfg *Config) *RPC {
	rpc := NewRPC(unixClient(h.socketPath, cfg.RequestTimeout), "http://youtubejs", cfg.Limiter)
	if cfg.ResponseBodyLimit > 0 {
		rpc.bodyLimit = cfg.ResponseBodyLimit
	}
	h.rpc = rpc
	h.health = unixClient(h.socketPath, cfg.HealthTimeout)
	h.endpoint = rpc.endpoint
	return rpc
}

func (h *Helper) waitForSocket(ctx context.Context) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		ready, err := h.waitForSocketEvent(ctx, ticker.C)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
	}
}

func (h *Helper) waitForSocketEvent(ctx context.Context, tick <-chan time.Time) (bool, error) {
	select {
	case <-ctx.Done():
		return false, fmt.Errorf("start youtube.js helper: wait for socket: %w", ctx.Err())
	case <-h.waited:
		return false, helperExitedBeforeReady(h.waitErr)
	case <-tick:
		return helperSocketReady(h.socketPath)
	}
}

func helperSocketReady(socketPath string) (bool, error) {
	info, err := os.Lstat(socketPath)
	if err != nil {
		if os.IsNotExist(err) || helperStarting(err) {
			return false, nil
		}
		return false, fmt.Errorf("start youtube.js helper: inspect socket: %w", err)
	}
	if info.Mode().Type() != os.ModeSocket && !info.Mode().IsRegular() {
		return false, fmt.Errorf("start youtube.js helper: unexpected socket path type")
	}
	return true, nil
}

func createRuntimeDir(base string) (runtimeDir, socketPath string, resultErr error) {
	dir, err := os.MkdirTemp(base, helperRuntimeDirPrefix)
	if err != nil {
		return "", "", fmt.Errorf("start youtube.js helper: create runtime dir: %w", err)
	}
	if err := finalizeRuntimeDir(dir); err != nil {
		return "", "", errors.Join(err, removeRuntimeDir(dir))
	}
	socketPath = filepath.Join(dir, helperSocketName)
	if _, err := os.Lstat(socketPath); err == nil {
		return "", "", errors.Join(fmt.Errorf("start youtube.js helper: socket path already exists"), removeRuntimeDir(dir))
	} else if !os.IsNotExist(err) {
		return "", "", errors.Join(fmt.Errorf("start youtube.js helper: inspect socket path: %w", err), removeRuntimeDir(dir))
	}
	if len(socketPath) > maxUnixSocketPathBytes {
		return "", "", errors.Join(fmt.Errorf("start youtube.js helper: socket path exceeds %d bytes", maxUnixSocketPathBytes), removeRuntimeDir(dir))
	}
	return dir, socketPath, nil
}

func removeRuntimeDir(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove youtube.js helper runtime: %w", err)
	}
	return nil
}

func finalizeRuntimeDir(dir string) error {
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // 비공개 디렉터리 탐색에는 소유자 실행 권한이 필요하다.
		return fmt.Errorf("start youtube.js helper: chmod runtime dir: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("start youtube.js helper: resolve runtime dir: %w", err)
	}
	if resolved != dir {
		return fmt.Errorf("start youtube.js helper: runtime dir must not be a symlink")
	}
	return nil
}

func unixClient(socketPath string, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultHelperTimeout
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
	}
}

func helperStarting(err error) bool {
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED)
}

func helperExitedBeforeReady(waitErr error) error {
	if waitErr == nil {
		return fmt.Errorf("start youtube.js helper: process exited before ready")
	}
	return fmt.Errorf("start youtube.js helper: process exited before ready: %w", waitErr)
}

func withOptionalTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func isFinished(err error) bool {
	return err != nil && strings.Contains(err.Error(), "process already finished")
}
