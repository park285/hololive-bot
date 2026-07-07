package delivery

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

const (
	appEnvProduction                        = "production"
	irisBaseURLAllowedHostsEnv              = "IRIS_BASE_URL_ALLOWED_HOSTS"
	irisBaseURLFileSkipStatChecksEnv        = "IRIS_BASE_URL_FILE_SKIP_STAT_CHECKS"
	irisH3ServerNameEnv                     = "IRIS_H3_SERVER_NAME"
	runtimeIrisBaseURLFileMaxAllowedPerms   = 0o644
	runtimeIrisBaseURLFileWorldWritablePerm = 0o002
)

type runtimeIrisBaseURLValidationOptions struct {
	allowUnconfiguredHost bool
	transport             string
	warnUnvalidatedHost   func(string)
}

func validateRuntimeIrisBaseURL(raw string) (string, error) {
	return validateRuntimeIrisBaseURLWithOptions(raw, runtimeIrisBaseURLValidationOptions{})
}

func validateRuntimeIrisBaseURLFileOverride(raw, transport string, warnUnvalidatedHost func(string)) (string, error) {
	return validateRuntimeIrisBaseURLWithOptions(raw, runtimeIrisBaseURLValidationOptions{
		allowUnconfiguredHost: true,
		transport:             transport,
		warnUnvalidatedHost:   warnUnvalidatedHost,
	})
}

func validateRuntimeIrisBaseURLWithOptions(raw string, opts runtimeIrisBaseURLValidationOptions) (string, error) {
	baseURL, parsed, err := parseRuntimeIrisBaseURL(raw)
	if err != nil {
		return "", err
	}
	if err := validateRuntimeIrisBaseURLScheme(parsed); err != nil {
		return "", err
	}
	if err := validateRuntimeIrisBaseURLShape(parsed, opts); err != nil {
		return "", err
	}
	if err := validateRuntimeIrisTransportScheme(runtimeIrisValidationTransport(opts.transport), parsed); err != nil {
		return "", err
	}

	return normalizeRuntimeIrisBaseURL(baseURL, parsed), nil
}

func parseRuntimeIrisBaseURL(raw string) (string, *url.URL, error) {
	baseURL := strings.TrimSpace(raw)
	if baseURL == "" {
		return "", nil, fmt.Errorf("base URL is empty")
	}

	parsed, err := url.ParseRequestURI(baseURL)
	if err != nil {
		return "", nil, err
	}
	return baseURL, parsed, nil
}

func validateRuntimeIrisBaseURLScheme(parsed *url.URL) error {
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported IRIS_BASE_URL_FILE URL scheme: %q", parsed.Scheme)
	}
	return nil
}

func validateRuntimeIrisBaseURLShape(parsed *url.URL, opts runtimeIrisBaseURLValidationOptions) error {
	if parsed.Host == "" {
		return fmt.Errorf("base URL host is empty")
	}
	if parsed.User != nil {
		return fmt.Errorf("IRIS_BASE_URL_FILE URL must not include userinfo")
	}
	if err := validateRuntimeIrisBaseURLPort(parsed); err != nil {
		return err
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return fmt.Errorf("IRIS_BASE_URL_FILE URL path must be empty")
	}
	if parsed.RawQuery != "" {
		return fmt.Errorf("IRIS_BASE_URL_FILE URL must not include query")
	}
	if parsed.Fragment != "" {
		return fmt.Errorf("IRIS_BASE_URL_FILE URL must not include fragment")
	}
	return validateRuntimeIrisBaseURLHost(parsed.Hostname(), opts)
}

func validateRuntimeIrisBaseURLPort(parsed *url.URL) error {
	if parsed.Port() != "" {
		return nil
	}
	if runtimeIrisBaseURLHostHasPortSeparator(parsed.Host) {
		return fmt.Errorf("IRIS_BASE_URL_FILE URL port must be numeric")
	}
	return nil
}

func runtimeIrisBaseURLHostHasPortSeparator(host string) bool {
	if strings.HasPrefix(host, "[") {
		return strings.Contains(host, "]:")
	}
	return strings.Contains(host, ":")
}

func normalizeRuntimeIrisBaseURL(baseURL string, parsed *url.URL) string {
	if parsed.Path == "/" {
		return strings.TrimSuffix(baseURL, "/")
	}
	return baseURL
}

func validateRuntimeIrisBaseURLHost(host string, opts runtimeIrisBaseURLValidationOptions) error {
	normalizedHost := normalizeRuntimeIrisHost(host)
	if normalizedHost == "" {
		return fmt.Errorf("IRIS_BASE_URL_FILE URL host is empty")
	}

	allowedHosts := runtimeIrisAllowedBaseURLHosts()
	if _, ok := allowedHosts[normalizedHost]; ok {
		return nil
	}
	if opts.allowUnconfiguredHost && !runtimeIrisBaseURLHostAllowlistConfigured() {
		if opts.warnUnvalidatedHost != nil {
			opts.warnUnvalidatedHost(host)
		}
		return nil
	}
	return fmt.Errorf("IRIS_BASE_URL_FILE host %q must match %s or %s", host, irisH3ServerNameEnv, irisBaseURLAllowedHostsEnv)
}

func runtimeIrisBaseURLHostAllowlistConfigured() bool {
	return strings.TrimSpace(os.Getenv(irisH3ServerNameEnv)) != "" ||
		strings.TrimSpace(os.Getenv(irisBaseURLAllowedHostsEnv)) != ""
}

func runtimeIrisAllowedBaseURLHosts() map[string]struct{} {
	allowedHosts := make(map[string]struct{})
	for _, rawHost := range append(
		[]string{os.Getenv(irisH3ServerNameEnv)},
		strings.Split(os.Getenv(irisBaseURLAllowedHostsEnv), ",")...,
	) {
		host := normalizeRuntimeIrisHost(rawHost)
		if host == "" {
			continue
		}
		allowedHosts[host] = struct{}{}
	}
	return allowedHosts
}

func normalizeRuntimeIrisHost(raw string) string {
	host := strings.ToLower(strings.TrimSpace(raw))
	if host == "" {
		return ""
	}

	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "."), "[")
	host = strings.TrimSuffix(host, "]")
	return host
}

func shouldValidateRuntimeIrisBaseURLFileStat() bool {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), appEnvProduction) {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(irisBaseURLFileSkipStatChecksEnv)), "true")
}

func normalizeRuntimeIrisBaseURLFilePath(path string, strict bool) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}
	if strict {
		if filepath.Clean(path) != path {
			return "", fmt.Errorf("path must be clean")
		}
		if runtimeIrisBaseURLFilePathContainsDotDot(path) {
			return "", fmt.Errorf("path must not include .. segments")
		}
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	return filepath.Clean(absolutePath), nil
}

func runtimeIrisBaseURLFilePathContainsDotDot(path string) bool {
	return slices.Contains(strings.Split(filepath.ToSlash(path), "/"), "..")
}

func validateRuntimeIrisBaseURLFileStat(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("file must not be a symlink")
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("file must be regular")
	}

	perm := info.Mode().Perm()
	if perm&^runtimeIrisBaseURLFileMaxAllowedPerms != 0 || perm&runtimeIrisBaseURLFileWorldWritablePerm != 0 {
		return fmt.Errorf("file permission %04o exceeds 0644", perm)
	}
	if err := validateRuntimeIrisBaseURLFileOwner(info); err != nil {
		return err
	}
	if err := validateRuntimeIrisBaseURLFileContainment(path); err != nil {
		return err
	}
	return nil
}

func validateRuntimeIrisBaseURLFileOwner(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}

	fileUID := stat.Uid
	euid := os.Geteuid()
	if euid < 0 || euid > math.MaxUint32 {
		return fmt.Errorf("current uid %d is outside uint32 range", euid)
	}
	currentUID := uint32(euid)
	if fileUID != 0 && fileUID != currentUID {
		return fmt.Errorf("file owner uid %d must be root or current uid %d", fileUID, currentUID)
	}
	return nil
}

func validateRuntimeIrisBaseURLFileContainment(path string) error {
	if err := validateRuntimeIrisBaseURLFileParentPath(path); err != nil {
		return err
	}
	canonicalDir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("resolve file directory: %w", err)
	}
	canonicalPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("resolve file path: %w", err)
	}

	rel, err := filepath.Rel(canonicalDir, canonicalPath)
	if err != nil {
		return fmt.Errorf("check file containment: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("file must stay inside its configured directory")
	}
	return nil
}

func validateRuntimeIrisBaseURLFileParentPath(path string) error {
	parent := filepath.Clean(filepath.Dir(path))
	current, rest := runtimeIrisBaseURLParentPathStart(parent)

	for _, part := range runtimeIrisBaseURLParentPathParts(rest) {
		current = filepath.Join(current, part)
		if err := validateRuntimeIrisBaseURLParentPathComponent(current); err != nil {
			return err
		}
	}
	return nil
}

func runtimeIrisBaseURLParentPathStart(parent string) (value0, value1 string) {
	volume := filepath.VolumeName(parent)
	rest := strings.TrimPrefix(parent, volume)
	separator := string(os.PathSeparator)

	if strings.HasPrefix(rest, separator) {
		return volume + separator, strings.TrimLeft(rest, separator)
	}
	if volume != "" {
		return volume, rest
	}
	return ".", rest
}

func runtimeIrisBaseURLParentPathParts(rest string) []string {
	parts := make([]string, 0)
	for part := range strings.SplitSeq(rest, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		parts = append(parts, part)
	}
	return parts
}

func validateRuntimeIrisBaseURLParentPathComponent(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat parent path component %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("parent directory path must not include symlink: %s", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("parent path component must be directory: %s", path)
	}
	return nil
}
