package delivery

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/park285/iris-client-go/v4/iris"

	"github.com/kapu/hololive-shared/pkg/config/settings"
)

// runtimeIrisBaseURLResolver는 생성 시 받은 설정만으로 Base URL을 해석한다. 해석 중 env를 다시
// 읽지 않으므로 기동 때 검증한 정책과 실제로 쓰는 정책이 갈라지지 않는다. URL 파일 내용만 동적으로 다시 읽는다.
type runtimeIrisBaseURLResolver struct {
	fallbackBaseURL  string
	baseURLFilePath  string
	allowedHosts     map[string]struct{}
	validateFileStat bool
}

func newRuntimeIrisBaseURLResolver(config settings.IrisConfig) (*runtimeIrisBaseURLResolver, error) {
	resolver := &runtimeIrisBaseURLResolver{
		fallbackBaseURL:  strings.TrimSpace(config.BaseURL),
		baseURLFilePath:  strings.TrimSpace(config.BaseURLFile),
		allowedHosts:     make(map[string]struct{}),
		validateFileStat: config.ValidateBaseURLFileStat,
	}

	if resolver.fallbackBaseURL == "" && resolver.baseURLFilePath == "" {
		return nil, errors.New("IRIS_BASE_URL or IRIS_BASE_URL_FILE is required")
	}

	for _, rawHost := range append([]string{config.H3ServerName}, config.BaseURLAllowedHosts...) {
		if host := normalizeRuntimeIrisHost(rawHost); host != "" {
			resolver.allowedHosts[host] = struct{}{}
		}
	}

	// 허용 집합이 비면 URL 파일의 모든 host를 거절하게 되므로 설정 오류로 먼저 드러낸다(fail-closed).
	if resolver.baseURLFilePath != "" && len(resolver.allowedHosts) == 0 {
		return nil, fmt.Errorf("IRIS_BASE_URL_FILE requires %s or %s", irisH3ServerNameEnv, irisBaseURLAllowedHostsEnv)
	}

	return resolver, nil
}

func (r *runtimeIrisBaseURLResolver) resolve() (string, error) {
	if r.baseURLFilePath != "" {
		out, err := r.resolveFromFile()
		if err != nil {
			return out, fmt.Errorf("resolve from file: %w", err)
		}

		return out, nil
	}

	parsed, err := iris.ParseBaseEndpoint(r.fallbackBaseURL)
	if err != nil {
		return "", fmt.Errorf("parse canonical Iris base endpoint: %w", err)
	}

	return parsed.String(), nil
}

func (r *runtimeIrisBaseURLResolver) resolveFromFile() (string, error) {
	baseURLFilePath, err := normalizeRuntimeIrisBaseURLFilePath(r.baseURLFilePath, r.validateFileStat)
	if err != nil {
		return "", fmt.Errorf("validate IRIS_BASE_URL_FILE path: %w", err)
	}

	if r.validateFileStat {
		if validateErr := validateRuntimeIrisBaseURLFileStat(baseURLFilePath); validateErr != nil {
			return "", fmt.Errorf("validate IRIS_BASE_URL_FILE: %w", validateErr)
		}
	}

	raw, err := readRuntimeIrisBaseURLFile(baseURLFilePath)
	if err != nil {
		return "", fmt.Errorf("read IRIS_BASE_URL_FILE: %w", err)
	}

	baseURL, err := validateRuntimeIrisBaseURLFileOverride(string(raw), r.allowedHosts)
	if err != nil {
		return "", fmt.Errorf("validate IRIS_BASE_URL_FILE URL: %w", err)
	}

	return baseURL, nil
}

func readRuntimeIrisBaseURLFile(path string) ([]byte, error) {
	dir, name := filepath.Split(path)
	if name == "" || strings.ContainsRune(name, os.PathSeparator) {
		return nil, errors.New("invalid IRIS_BASE_URL_FILE basename")
	}

	out, err := fs.ReadFile(os.DirFS(dir), name)
	if err != nil {
		return out, fmt.Errorf("read file: %w", err)
	}

	return out, nil
}
