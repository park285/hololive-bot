package youtubejs

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

const ProtocolVersion int16 = 1

const (
	DefaultRequestBodyLimit  int64 = 64 << 10
	DefaultResponseBodyLimit int64 = 1 << 20
	DefaultMaxInflight             = 4
)

const (
	defaultHelperTimeout   = 30 * time.Second
	defaultHealthTimeout   = time.Second
	defaultShutdownTimeout = 3 * time.Second
	maxHelperInflight      = 64
	maxUnixSocketPathBytes = 100
	helperRuntimeDirPrefix = "youtubejs-helper-"
	helperSocketName       = "helper.sock"
	defaultHelperBodyLimit = DefaultResponseBodyLimit
)

type ProxyConfig struct {
	Enabled bool
	URL     string
}

type Config struct {
	NodePath          string
	ScriptPath        string
	RuntimeBaseDir    string
	StartupTimeout    time.Duration
	RequestTimeout    time.Duration
	HealthTimeout     time.Duration
	ShutdownTimeout   time.Duration
	RequestBodyLimit  int64
	ResponseBodyLimit int64
	MaxInflight       int
	Proxy             ProxyConfig
	Limiter           *ratelimiter.RateLimiter
	extraArgs         []string
}

func resolveConfig(config *Config) (Config, error) {
	if config == nil {
		return Config{}, fmt.Errorf("start youtube.js helper: config is not configured")
	}
	cfg := *config
	resolveHelperPaths(&cfg)
	applyConfigDefaults(&cfg)
	if err := absolutizeHelperPaths(&cfg); err != nil {
		return Config{}, err
	}
	if err := validateResolvedConfig(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func applyConfigDefaults(cfg *Config) {
	if strings.TrimSpace(cfg.RuntimeBaseDir) == "" {
		cfg.RuntimeBaseDir = os.TempDir()
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = defaultHelperTimeout
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = cfg.RequestTimeout
	}
	if cfg.HealthTimeout <= 0 {
		cfg.HealthTimeout = defaultHealthTimeout
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = defaultShutdownTimeout
	}
	if cfg.RequestBodyLimit <= 0 {
		cfg.RequestBodyLimit = DefaultRequestBodyLimit
	}
	if cfg.ResponseBodyLimit <= 0 {
		cfg.ResponseBodyLimit = DefaultResponseBodyLimit
	}
	cfg.Proxy.URL = strings.TrimSpace(cfg.Proxy.URL)
}

func validateResolvedConfig(cfg *Config) error {
	if cfg.NodePath == "" {
		return fmt.Errorf("start youtube.js helper: node binary is not configured")
	}
	if cfg.ScriptPath == "" {
		return fmt.Errorf("start youtube.js helper: helper script is not configured")
	}
	if cfg.MaxInflight < 1 || cfg.MaxInflight > maxHelperInflight {
		return fmt.Errorf("start youtube.js helper: max inflight must be between 1 and %d", maxHelperInflight)
	}
	if cfg.RequestBodyLimit <= 0 || cfg.ResponseBodyLimit <= 0 {
		return fmt.Errorf("start youtube.js helper: body limits must be positive")
	}
	if err := validateRuntimeBaseDir(cfg.RuntimeBaseDir); err != nil {
		return err
	}
	return validateProxy(cfg.Proxy)
}

func validateProxy(proxy ProxyConfig) error {
	if !proxy.Enabled {
		return validateDisabledProxy(proxy.URL)
	}
	if proxy.URL == "" {
		return fmt.Errorf("start youtube.js helper: proxy url is required when proxy is enabled")
	}
	parsed, err := url.Parse(proxy.URL)
	if err != nil {
		return fmt.Errorf("start youtube.js helper: proxy url is invalid")
	}
	if !validProxyURL(parsed) {
		return fmt.Errorf("start youtube.js helper: proxy url is invalid")
	}
	return nil
}

func validateDisabledProxy(proxyURL string) error {
	if proxyURL != "" {
		return fmt.Errorf("start youtube.js helper: proxy url must be empty when proxy is disabled")
	}
	return nil
}

func validProxyURL(parsed *url.URL) bool {
	validScheme := parsed.Scheme == "http" || parsed.Scheme == "https"
	validLocation := strings.TrimSpace(parsed.Host) != "" && (parsed.Path == "" || parsed.Path == "/")
	return validScheme && validLocation && parsed.RawQuery == "" && parsed.Fragment == ""
}

func validateRuntimeBaseDir(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return fmt.Errorf("start youtube.js helper: runtime base dir is not configured")
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("start youtube.js helper: resolve runtime base dir: %w", err)
		}
		path = abs
	}
	if filepath.Clean(path) != path {
		return fmt.Errorf("start youtube.js helper: runtime base dir path must be clean")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("start youtube.js helper: inspect runtime base dir: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("start youtube.js helper: runtime base dir must not be a symlink")
	}
	if !info.IsDir() {
		return fmt.Errorf("start youtube.js helper: runtime base dir must be a directory")
	}
	return nil
}

func resolveHelperPaths(cfg *Config) {
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
}

func absolutizeHelperPaths(cfg *Config) error {
	paths := []struct {
		value *string
		name  string
	}{
		{&cfg.NodePath, "node path"},
		{&cfg.ScriptPath, "script path"},
		{&cfg.RuntimeBaseDir, "runtime base dir"},
	}
	for _, path := range paths {
		if err := absolutizeHelperPath(path.value, path.name); err != nil {
			return err
		}
	}
	if cfg.RuntimeBaseDir != "" {
		cfg.RuntimeBaseDir = filepath.Clean(cfg.RuntimeBaseDir)
	}
	if cfg.NodePath == "" || cfg.ScriptPath == "" {
		return nil
	}
	resolvedNode, err := canonicalHelperFile(cfg.NodePath, "node binary")
	if err != nil {
		return err
	}
	resolvedScript, err := canonicalHelperFile(cfg.ScriptPath, "helper script")
	if err != nil {
		return err
	}
	cfg.NodePath = resolvedNode
	cfg.ScriptPath = resolvedScript
	return nil
}

func absolutizeHelperPath(value *string, name string) error {
	if *value == "" || filepath.IsAbs(*value) {
		return nil
	}
	abs, err := filepath.Abs(*value)
	if err != nil {
		return fmt.Errorf("start youtube.js helper: resolve %s: %w", name, err)
	}
	*value = abs
	return nil
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
	for _, candidate := range paths {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		resolved, err := canonicalHelperFile(absolute, "helper candidate")
		if err == nil {
			return resolved
		}
	}
	return ""
}

func canonicalHelperFile(path, name string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", fmt.Errorf("start youtube.js helper: %s path must be absolute and clean", name)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("start youtube.js helper: resolve %s path: %w", name, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("start youtube.js helper: inspect %s path: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("start youtube.js helper: %s path is not a regular file", name)
	}
	return resolved, nil
}
