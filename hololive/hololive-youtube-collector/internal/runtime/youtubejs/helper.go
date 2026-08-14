package youtubejs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
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

func NewRPC(httpClient *http.Client, endpoint string, limiter *ratelimiter.RateLimiter) *RPC {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHelperTimeout}
	}
	return &RPC{
		http:      httpClient,
		endpoint:  strings.TrimRight(endpoint, "/"),
		limiter:   limiter,
		bodyLimit: defaultHelperBodyLimit,
	}
}

func (c *RPC) SetProxyEnabled(enabled bool) bool {
	if c == nil {
		return false
	}
	c.proxyOn.Store(enabled)
	return true
}

func (c *RPC) ProxyEnabled() bool {
	return c != nil && c.proxyOn.Load()
}

func (c *RPC) SetProxyURL(proxyURL string) {
	if c == nil {
		return
	}
	c.proxyURL = strings.TrimSpace(proxyURL)
}

func (c *RPC) FetchCommunity(ctx context.Context, request CommunityRequest) (CommunityResult, error) {
	var result CommunityResult
	if err := c.doJSON(ctx, "/v1/community", &request, &result); err != nil {
		return CommunityResult{}, err
	}
	if err := resultError(result.Error); err != nil {
		return CommunityResult{}, err
	}
	for _, post := range result.Posts {
		if post == nil {
			continue
		}
		if post.PublishedAt == nil && post.PublishedText != "" {
			if publishedAt, ok := parser.NormalizePublishedAtCandidate(post.PublishedText); ok {
				post.PublishedAt = publishedAt
			}
		}
	}
	return result, nil
}

func (c *RPC) FetchContent(ctx context.Context, request ContentRequest) (ContentResult, error) {
	var result ContentResult
	if err := c.doJSON(ctx, "/v1/content", &request, &result); err != nil {
		return ContentResult{}, err
	}
	if err := resultError(result.Error); err != nil {
		return ContentResult{}, err
	}
	return result, nil
}

func (c *RPC) FetchChannel(ctx context.Context, request ChannelRequest) (ChannelResult, error) {
	var result ChannelResult
	if err := c.doJSON(ctx, "/v1/channel", &request, &result); err != nil {
		return ChannelResult{}, err
	}
	if err := resultError(result.Error); err != nil {
		return ChannelResult{}, err
	}
	return result, nil
}

func (c *RPC) FetchViewer(ctx context.Context, request ViewerRequest) (ViewerResult, error) {
	var result ViewerResult
	if err := c.doJSON(ctx, "/v1/viewer", &request, &result); err != nil {
		return ViewerResult{}, err
	}
	if err := resultError(result.Error); err != nil {
		return ViewerResult{}, err
	}
	return result, nil
}

func resultError(message string) error {
	if strings.TrimSpace(message) == "" {
		return nil
	}
	return collecterr.New(collecterr.Failed, "youtube.js helper: "+message)
}

func (c *RPC) doJSON(ctx context.Context, path string, request any, response any) error {
	if c == nil || c.http == nil {
		return collecterr.New(collecterr.Failed, "youtube.js client is not configured")
	}
	if err := c.waitLimiter(ctx); err != nil {
		return err
	}
	if setter, ok := request.(proxySetter); ok && c.ProxyEnabled() {
		setter.setProxyURL(c.proxyURL)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return collecterr.Wrap(collecterr.Failed, fmt.Errorf("marshal youtube.js helper request: %w", err))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+path, bytes.NewReader(raw))
	if err != nil {
		return collecterr.Wrap(collecterr.Failed, fmt.Errorf("build youtube.js helper request: %w", err))
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return collecterr.FromContext(fmt.Errorf("youtube.js helper: %w", err))
	}
	defer resp.Body.Close()
	limit := c.bodyLimit
	if limit <= 0 {
		limit = defaultHelperBodyLimit
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return collecterr.FromContext(fmt.Errorf("read youtube.js helper: %w", err))
	}
	if int64(len(payload)) > limit {
		return collecterr.New(collecterr.ParserDrift, "youtube.js helper response exceeds body limit")
	}
	if err := json.Unmarshal(payload, response); err != nil {
		return collecterr.Wrap(collecterr.ParserDrift, fmt.Errorf("decode youtube.js helper: %w", err))
	}
	if resp.StatusCode != http.StatusOK {
		return helperStatusError(resp.StatusCode, payload)
	}
	return nil
}

func helperStatusError(status int, payload []byte) error {
	var decoded struct {
		Error string `json:"error"`
		Code  string `json:"error_code"`
	}
	_ = json.Unmarshal(payload, &decoded)
	errText := strings.TrimSpace(decoded.Error)
	if errText == "" {
		errText = strings.TrimSpace(string(payload))
	}
	code := decoded.Code
	if code == "" {
		code = collecterr.Failed
	}
	return collecterr.Wrap(code, fmt.Errorf("youtube.js helper status %d: %s", status, errText))
}

func (c *RPC) waitLimiter(ctx context.Context) error {
	if c.limiter == nil {
		return nil
	}
	if err := c.limiter.Wait(ctx); err != nil {
		return collecterr.FromContext(fmt.Errorf("wait for youtube.js rate limiter: %w", err))
	}
	return nil
}

type proxySetter interface {
	setProxyURL(string)
}

func (r *CommunityRequest) setProxyURL(value string) { r.ProxyURL = value }
func (r *ContentRequest) setProxyURL(value string)   { r.ProxyURL = value }
func (r *ChannelRequest) setProxyURL(value string)   { r.ProxyURL = value }
func (r *ViewerRequest) setProxyURL(value string)    { r.ProxyURL = value }

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
	var stopErr error
	if h.cmd != nil && h.cmd.Process != nil {
		if err := h.cmd.Process.Kill(); err != nil && !isFinished(err) {
			stopErr = fmt.Errorf("stop youtube.js helper: %w", err)
		}
	}
	if h.waited != nil {
		select {
		case <-h.waited:
		case <-time.After(3 * time.Second):
			if stopErr == nil {
				stopErr = fmt.Errorf("stop youtube.js helper: wait timed out")
			}
		}
	}
	if h.socketPath != "" {
		if err := os.Remove(h.socketPath); err != nil && !os.IsNotExist(err) && stopErr == nil {
			stopErr = fmt.Errorf("remove youtube.js helper socket: %w", err)
		}
	}
	return stopErr
}

func (h *Helper) waitReady(ctx context.Context) error {
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("start youtube.js helper: wait for socket: %w", ctx.Err())
		case <-h.waited:
			if h.waitErr == nil {
				return fmt.Errorf("start youtube.js helper: process exited before ready")
			}
			return fmt.Errorf("start youtube.js helper: process exited before ready: %w", h.waitErr)
		case <-ticker.C:
			if h.health == nil {
				return collecterr.New(collecterr.Failed, "youtube.js helper is not configured")
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.endpoint+"/health", nil)
			if err != nil {
				return fmt.Errorf("start youtube.js helper: health request: %w", err)
			}
			resp, err := h.health.Do(req)
			if err != nil {
				continue
			}
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
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
	if cfg.NodePath == "" {
		return Config{}, fmt.Errorf("start youtube.js helper: node binary is not configured")
	}
	if cfg.ScriptPath == "" {
		return Config{}, fmt.Errorf("start youtube.js helper: helper script is not configured")
	}
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
