package delivery

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/park285/iris-client-go/v2/iris"
)

type runtimeIrisBaseURLResolver struct {
	fallbackBaseURL string
	baseURLFilePath string
	transport       string
	logger          *slog.Logger
	warnOnce        sync.Once
}

func (r *runtimeIrisBaseURLResolver) resolve() (string, error) {
	if r.baseURLFilePath != "" {
		out, err := r.resolveFromFile()
		if err != nil {
			return out, fmt.Errorf("resolve from file: %w", err)
		}

		return out, nil
	}

	out, err := validateHTTPBaseURL(r.fallbackBaseURL, r.transport)
	if err != nil {
		return out, fmt.Errorf("validate HTTP base URL: %w", err)
	}

	return out, nil
}

func (r *runtimeIrisBaseURLResolver) resolveFromFile() (string, error) {
	validateStat := shouldValidateRuntimeIrisBaseURLFileStat()

	baseURLFilePath, err := normalizeRuntimeIrisBaseURLFilePath(r.baseURLFilePath, validateStat)
	if err != nil {
		return "", fmt.Errorf("validate IRIS_BASE_URL_FILE path: %w", err)
	}

	if validateStat {
		if validateErr := validateRuntimeIrisBaseURLFileStat(baseURLFilePath); validateErr != nil {
			return "", fmt.Errorf("validate IRIS_BASE_URL_FILE: %w", validateErr)
		}
	}

	raw, err := readRuntimeIrisBaseURLFile(baseURLFilePath)
	if err != nil {
		return "", fmt.Errorf("read IRIS_BASE_URL_FILE: %w", err)
	}

	baseURL, err := validateRuntimeIrisBaseURLFileOverride(string(raw), r.transport, r.warnBaseURLHostUnvalidated)
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

func (r *runtimeIrisBaseURLResolver) warnBaseURLHostUnvalidated(host string) {
	if r.logger == nil {
		return
	}

	r.warnOnce.Do(func() {
		r.logger.Warn("IRIS_BASE_URL_FILE host is unvalidated because no Iris base URL allowlist is configured",
			slog.String("host", host),
			slog.String("path", r.baseURLFilePath),
			slog.String("allowlist_env", irisH3ServerNameEnv+","+irisBaseURLAllowedHostsEnv),
		)
	})
}

func validateHTTPBaseURL(raw string, explicitTransport ...string) (string, error) {
	parsed, err := iris.ParseBaseEndpoint(raw)
	if err != nil {
		return "", fmt.Errorf("parse canonical Iris base endpoint: %w", err)
	}

	if err := validateRuntimeIrisTransportScheme(runtimeIrisValidationTransport(firstRuntimeIrisTransport(explicitTransport)), parsed); err != nil {
		return "", fmt.Errorf("validate runtime iris transport scheme: %w", err)
	}

	return parsed.String(), nil
}

func firstRuntimeIrisTransport(values []string) string {
	if len(values) == 0 {
		return ""
	}

	return values[0]
}
