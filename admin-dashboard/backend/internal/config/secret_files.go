package config

import (
	"fmt"
	"os"
	"strings"
)

const (
	maxSecretFileBytes = 64 << 10
	minSessionSecretBytes = 32
)

type secretFileSpec struct {
	fileEnv   string
	valueEnv  string
	aliases   []string
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
func LoadSecure() (*Config, error) {
	restore, err := applySecretFiles()
	if err != nil {
		return nil, err
	}
	defer restore()

	cfg, err := Load()
	if err != nil {
		return nil, err
	}
	if len([]byte(cfg.SessionSecret)) < minSessionSecretBytes {
		return nil, fmt.Errorf("SESSION_SECRET must be at least %d bytes", minSessionSecretBytes)
	}
	return cfg, nil
}

func applySecretFiles() (func(), error) {
	type originalValue struct {
		value string
		set   bool
	}
	originals := make(map[string]originalValue, len(adminSecretFiles))
	applied := make([]string, 0, len(adminSecretFiles))

	restore := func() {
		for i := len(applied) - 1; i >= 0; i-- {
			key := applied[i]
			original := originals[key]
			if original.set {
				_ = os.Setenv(key, original.value)
			} else {
				_ = os.Unsetenv(key)
			}
		}
	}

	for _, spec := range adminSecretFiles {
		path := strings.TrimSpace(os.Getenv(spec.fileEnv))
		if path == "" {
			continue
		}
		if directSecretConfigured(spec) {
			restore()
			return nil, fmt.Errorf("config: %s cannot be combined with %s or its aliases", spec.fileEnv, spec.valueEnv)
		}
		value, err := readSecretFile(path)
		if err != nil {
			restore()
			return nil, fmt.Errorf("config: read %s: %w", spec.fileEnv, err)
		}
		original, set := os.LookupEnv(spec.valueEnv)
		originals[spec.valueEnv] = originalValue{value: original, set: set}
		if err := os.Setenv(spec.valueEnv, value); err != nil {
			restore()
			return nil, fmt.Errorf("config: materialize %s: %w", spec.fileEnv, err)
		}
		applied = append(applied, spec.valueEnv)
	}
	return restore, nil
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

func readSecretFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > maxSecretFileBytes {
		return "", fmt.Errorf("%s exceeds %d bytes", path, maxSecretFileBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(data) > maxSecretFileBytes {
		return "", fmt.Errorf("%s exceeds %d bytes", path, maxSecretFileBytes)
	}
	value := strings.TrimSuffix(string(data), "\n")
	value = strings.TrimSuffix(value, "\r")
	if strings.ContainsAny(value, "\x00\r\n") {
		return "", fmt.Errorf("%s contains embedded NUL or newline", path)
	}
	return value, nil
}
