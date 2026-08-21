package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	maxSecretFileBytes    = 64 << 10
	minSessionSecretBytes = 32
)

type secretFileSpec struct {
	fileEnv  string
	valueEnv string
	aliases  []string
}

type secretEnvValue struct {
	value string
	set   bool
}

var adminSecretFiles = []secretFileSpec{
	{fileEnv: "ADMIN_PASS_HASH_FILE", valueEnv: "ADMIN_PASS_HASH", aliases: []string{"ADMIN_PASS_BCRYPT"}},
	{fileEnv: "SESSION_SECRET_FILE", valueEnv: "SESSION_SECRET", aliases: []string{"ADMIN_SECRET_KEY"}},
	{fileEnv: "VALKEY_URL_FILE", valueEnv: "VALKEY_URL"},
	{fileEnv: "HOLO_BOT_API_KEY_FILE", valueEnv: "HOLO_BOT_API_KEY", aliases: []string{"API_SECRET_KEY"}},
}

// LoadSecure applies Docker/Kubernetes-style *_FILE secrets only for the duration
// of Config loading, then removes the materialized values from the process
// environment. The returned Config owns ordinary Go strings; Docker Config.Env
// therefore contains only file paths, not the secret values themselves.
func LoadSecure() (cfg *Config, err error) {
	restore, err := applySecretFiles()
	if err != nil {
		return nil, err
	}
	defer func() {
		if restoreErr := restore(); restoreErr != nil {
			cfg = nil
			err = errors.Join(err, restoreErr)
		}
	}()

	cfg, err = Load()
	if err != nil {
		return nil, err
	}
	if len([]byte(cfg.SessionSecret)) < minSessionSecretBytes {
		return nil, fmt.Errorf("SESSION_SECRET must be at least %d bytes", minSessionSecretBytes)
	}
	return cfg, nil
}

func applySecretFiles() (func() error, error) {
	originals := make(map[string]secretEnvValue, len(adminSecretFiles))
	applied := make([]string, 0, len(adminSecretFiles))

	for _, spec := range adminSecretFiles {
		path := strings.TrimSpace(os.Getenv(spec.fileEnv))
		if path == "" {
			continue
		}
		if directSecretConfigured(spec) {
			return nil, errors.Join(
				fmt.Errorf("config: %s cannot be combined with %s or its aliases", spec.fileEnv, spec.valueEnv),
				restoreSecretEnv(applied, originals),
			)
		}
		value, err := readSecretFile(path)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("config: read %s: %w", spec.fileEnv, err), restoreSecretEnv(applied, originals))
		}
		original, set := os.LookupEnv(spec.valueEnv)
		originals[spec.valueEnv] = secretEnvValue{value: original, set: set}
		if err := os.Setenv(spec.valueEnv, value); err != nil {
			return nil, errors.Join(fmt.Errorf("config: materialize %s: %w", spec.fileEnv, err), restoreSecretEnv(applied, originals))
		}
		applied = append(applied, spec.valueEnv)
	}
	return func() error { return restoreSecretEnv(applied, originals) }, nil
}

func restoreSecretEnv(applied []string, originals map[string]secretEnvValue) error {
	var restoreErr error
	for i := len(applied) - 1; i >= 0; i-- {
		key := applied[i]
		original := originals[key]
		if original.set {
			if err := os.Setenv(key, original.value); err != nil {
				restoreErr = errors.Join(restoreErr, fmt.Errorf("config: restore %s: %w", key, err))
			}
		} else if err := os.Unsetenv(key); err != nil {
			restoreErr = errors.Join(restoreErr, fmt.Errorf("config: unset %s: %w", key, err))
		}
	}
	return restoreErr
}

func directSecretConfigured(spec secretFileSpec) bool {
	if value, ok := os.LookupEnv(spec.valueEnv); ok && value != "" {
		return true
	}
	for _, alias := range spec.aliases {
		if value, ok := os.LookupEnv(alias); ok && value != "" {
			return true
		}
	}
	return false
}

func readSecretFile(path string) (value string, err error) {
	info, err := os.Lstat(path) // #nosec G703 -- the administrator-configured secret path is checked before and after opening
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > maxSecretFileBytes {
		return "", fmt.Errorf("%s exceeds %d bytes", path, maxSecretFileBytes)
	}
	file, err := os.Open(path) // #nosec G304,G703 -- file identity is compared with the preceding non-symlink Lstat result
	if err != nil {
		return "", err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close %s: %w", path, closeErr))
		}
	}()
	openedInfo, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return "", fmt.Errorf("%s changed while opening", path)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxSecretFileBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxSecretFileBytes {
		return "", fmt.Errorf("%s exceeds %d bytes", path, maxSecretFileBytes)
	}
	value = strings.TrimSuffix(string(data), "\n")
	value = strings.TrimSuffix(value, "\r")
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("%s contains embedded NUL or newline", path)
	}
	return value, nil
}
