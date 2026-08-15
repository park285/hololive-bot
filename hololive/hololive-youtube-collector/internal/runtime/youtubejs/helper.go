package youtubejs

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

const defaultHelperTimeout = 30 * time.Second
const defaultHelperBodyLimit = 1 << 20

type Config struct {
	NodePath   string
	ScriptPath string
	SocketPath string
	ProxyURL   string
	ProxyOn    bool
	Timeout    time.Duration
	BodyLimit  int64
	Limiter    *ratelimiter.RateLimiter
}

type RPC struct {
	http      *http.Client
	endpoint  string
	proxyURL  string
	proxyOn   atomic.Bool
	limiter   *ratelimiter.RateLimiter
	bodyLimit int64
}

type Helper struct {
	cmd        *exec.Cmd
	socketPath string
	waited     chan struct{}
	waitErr    error
	health     *http.Client
	endpoint   string
}

func (h *Helper) Done() <-chan struct{} {
	if h == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return h.waited
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

func Start(ctx context.Context, cfg Config) (*Helper, *RPC, error) {
	cfg, err := resolveConfig(cfg)
	if err != nil {
		return nil, nil, err
	}
	if err := os.Remove(cfg.SocketPath); err != nil && !os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("start youtube.js helper: remove stale socket: %w", err)
	}
	cmd := &exec.Cmd{
		Path:   cfg.NodePath,
		Args:   []string{cfg.NodePath, cfg.ScriptPath, "--socket", cfg.SocketPath},
		Stdout: os.Stderr,
		Stderr: os.Stderr,
		Env:    helperProcessEnv(os.Environ()),
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("start youtube.js helper: %w", err)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultHelperTimeout
	}
	rpc := NewRPC(unixClient(cfg.SocketPath, timeout), "http://youtubejs", cfg.Limiter)
	if cfg.BodyLimit > 0 {
		rpc.bodyLimit = cfg.BodyLimit
	}
	rpc.SetProxyURL(cfg.ProxyURL)
	rpc.SetProxyEnabled(cfg.ProxyOn)
	helper := &Helper{
		cmd:        cmd,
		socketPath: cfg.SocketPath,
		waited:     make(chan struct{}),
		health:     rpc.http,
		endpoint:   rpc.endpoint,
	}
	go func() {
		helper.waitErr = cmd.Wait()
		close(helper.waited)
	}()
	if err := helper.waitReady(ctx); err != nil {
		_ = helper.Close()
		return nil, nil, err
	}
	return helper, rpc, nil
}

func (h *Helper) Close() error {
	if h == nil {
		return nil
	}
	stopErr := h.killProcess()
	if waitErr := h.waitExit(); stopErr == nil {
		stopErr = waitErr
	}
	if removeErr := h.removeSocket(); stopErr == nil {
		stopErr = removeErr
	}
	return stopErr
}

func (h *Helper) killProcess() error {
	if h.cmd == nil || h.cmd.Process == nil {
		return nil
	}
	if err := h.cmd.Process.Kill(); err != nil && !isFinished(err) {
		return fmt.Errorf("stop youtube.js helper: %w", err)
	}
	return nil
}

func (h *Helper) waitExit() error {
	if h.waited == nil {
		return nil
	}
	select {
	case <-h.waited:
		return nil
	case <-time.After(3 * time.Second):
		return fmt.Errorf("stop youtube.js helper: wait timed out")
	}
}

func (h *Helper) removeSocket() error {
	if h.socketPath == "" {
		return nil
	}
	if err := os.Remove(h.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove youtube.js helper socket: %w", err)
	}
	return nil
}

func (h *Helper) waitReady(ctx context.Context) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		ready, err := h.waitReadyOnce(ctx, ticker)
		if err != nil || ready {
			return err
		}
	}
}

func (h *Helper) waitReadyOnce(ctx context.Context, ticker *time.Ticker) (bool, error) {
	select {
	case <-ctx.Done():
		return false, fmt.Errorf("start youtube.js helper: wait for socket: %w", ctx.Err())
	case <-h.waited:
		return false, helperExitedBeforeReady(h.waitErr)
	case <-ticker.C:
		return h.probeHealth(ctx)
	}
}

func helperExitedBeforeReady(waitErr error) error {
	if waitErr == nil {
		return fmt.Errorf("start youtube.js helper: process exited before ready")
	}
	return fmt.Errorf("start youtube.js helper: process exited before ready: %w", waitErr)
}

func (h *Helper) Healthy(ctx context.Context) bool {
	ok, err := h.probeHealth(ctx)
	return err == nil && ok
}

func (h *Helper) probeHealth(ctx context.Context) (bool, error) {
	if h.health == nil {
		return false, collecterr.New(collecterr.Failed, "youtube.js helper is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.endpoint+"/health", nil)
	if err != nil {
		return false, fmt.Errorf("start youtube.js helper: health request: %w", err)
	}
	resp, err := h.health.Do(req)
	if err != nil {
		return false, nil
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
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

func resolveConfig(cfg Config) (Config, error) {
	cfg = resolveHelperPaths(cfg)
	if cfg.NodePath == "" {
		return Config{}, fmt.Errorf("start youtube.js helper: node binary is not configured")
	}
	if cfg.ScriptPath == "" {
		return Config{}, fmt.Errorf("start youtube.js helper: helper script is not configured")
	}
	return absolutizeHelperPaths(cfg)
}

func resolveHelperPaths(cfg Config) Config {
	if cfg.NodePath == "" {
		cfg.NodePath = firstExisting(
			os.Getenv("YOUTUBEJS_NODE"),
			"/usr/local/bin/node",
			"/nodejs/bin/node",
		)
	}
	if cfg.NodePath == "" {
		if found, err := exec.LookPath("node"); err == nil {
			cfg.NodePath = found
		}
	}
	if cfg.ScriptPath == "" {
		cfg.ScriptPath = firstExisting(
			os.Getenv("YOUTUBEJS_SCRIPT"),
			"youtubejs/src/server.mjs",
			"/app/youtubejs/src/server.mjs",
		)
	}
	if cfg.SocketPath == "" {
		cfg.SocketPath = strings.TrimSpace(os.Getenv("YOUTUBEJS_SOCKET"))
	}
	if cfg.SocketPath == "" {
		cfg.SocketPath = filepath.Join(os.TempDir(), "youtubejs-community.sock")
	}
	return cfg
}

func absolutizeHelperPaths(cfg Config) (Config, error) {
	if !filepath.IsAbs(cfg.NodePath) {
		abs, err := filepath.Abs(cfg.NodePath)
		if err != nil {
			return Config{}, fmt.Errorf("start youtube.js helper: resolve node path: %w", err)
		}
		cfg.NodePath = abs
	}
	if !filepath.IsAbs(cfg.ScriptPath) {
		abs, err := filepath.Abs(cfg.ScriptPath)
		if err != nil {
			return Config{}, fmt.Errorf("start youtube.js helper: resolve script path: %w", err)
		}
		cfg.ScriptPath = abs
	}
	return cfg, nil
}

func helperProcessEnv(parent []string) []string {
	allowed := map[string]struct{}{
		"HOME":                {},
		"LANG":                {},
		"LC_ALL":              {},
		"LC_CTYPE":            {},
		"NODE_ENV":            {},
		"NODE_EXTRA_CA_CERTS": {},
		"PATH":                {},
		"SSL_CERT_DIR":        {},
		"SSL_CERT_FILE":       {},
		"TMPDIR":              {},
		"TZ":                  {},
		"YOUTUBEJS_NODE":      {},
		"YOUTUBEJS_SCRIPT":    {},
		"YOUTUBEJS_SOCKET":    {},
	}
	filtered := make([]string, 0, len(allowed))
	for _, entry := range parent {
		key, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if _, ok := allowed[key]; ok {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func firstExisting(paths ...string) string {
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func isFinished(err error) bool {
	return err != nil && strings.Contains(err.Error(), "process already finished")
}
