package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"syscall"
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

var adminSecretFiles = []secretFileSpec{
	{fileEnv: "ADMIN_PASS_HASH_FILE", valueEnv: "ADMIN_PASS_HASH", aliases: []string{"ADMIN_PASS_BCRYPT"}},
	{fileEnv: "SESSION_SECRET_FILE", valueEnv: "SESSION_SECRET", aliases: []string{"ADMIN_SECRET_KEY"}},
	{fileEnv: "VALKEY_URL_FILE", valueEnv: "VALKEY_URL"},
	{fileEnv: "HOLO_BOT_API_KEY_FILE", valueEnv: "HOLO_BOT_API_KEY", aliases: []string{"API_SECRET_KEY"}},
}

func (in *inputValues) applySecretFiles() error {
	for _, spec := range adminSecretFiles {
		path := in.text(spec.fileEnv, "")
		if path == "" {
			continue
		}

		if in.first(append([]string{spec.valueEnv}, spec.aliases...)...) != "" {
			return fmt.Errorf("%s cannot be combined with %s or its aliases", spec.fileEnv, spec.valueEnv)
		}

		value, err := readSecretFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", spec.fileEnv, err)
		}

		in.values[spec.valueEnv] = value
	}

	return nil
}

func validateSecretMetadata(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return errors.New("secret file ownership is unavailable")
	}

	if stat.Uid != 0 && int(stat.Uid) != os.Geteuid() {
		return errors.New("secret file must be owned by root or the runtime uid")
	}

	if info.Mode().Perm()&0o400 == 0 || info.Mode().Perm()&0o137 != 0 {
		return errors.New("secret file permissions must restrict writes to its owner and reads to the runtime group")
	}

	if info.Mode().Perm()&0o040 != 0 {
		groups, err := os.Getgroups()
		if err != nil {
			return fmt.Errorf("read runtime groups: %w", err)
		}

		if int(stat.Gid) != os.Getegid() && !slices.Contains(groups, int(stat.Gid)) {
			return errors.New("secret file group must belong to the runtime")
		}
	}

	return nil
}

func readSecretFile(path string) (string, error) {
	info, err := statSecretFile(path)
	if err != nil {
		return "", fmt.Errorf("stat secret file: %w", err)
	}

	data, err := readVerifiedSecretFile(path, info)
	if err != nil {
		return "", fmt.Errorf("read verified secret file: %w", err)
	}

	out, err := sanitizeSecretValue(path, data)
	if err != nil {
		return out, fmt.Errorf("sanitize secret value: %w", err)
	}

	return out, nil
}

func statSecretFile(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path) // #nosec G703 -- the administrator-configured secret path is checked before and after opening
	if err != nil {
		return nil, fmt.Errorf("lstat: %w", err)
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}

	if err := validateSecretMetadata(info); err != nil {
		return nil, err
	}

	if info.Size() > maxSecretFileBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", path, maxSecretFileBytes)
	}

	return info, nil
}

func readVerifiedSecretFile(path string, info os.FileInfo) (data []byte, err error) {
	// NOFOLLOW와 NONBLOCK은 검사 사이 symlink/FIFO 교체가 비밀 노출·무기한 대기로 이어지는 것을 막습니다.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0) // #nosec G304,G703 -- configured path; no-follow open and identity/mode checks bound the read
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close %s: %w", path, closeErr))
		}
	}()

	openedInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}

	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return nil, fmt.Errorf("%s changed while opening", path)
	}

	if metadataErr := validateSecretMetadata(openedInfo); metadataErr != nil {
		return nil, metadataErr
	}

	out, err := io.ReadAll(io.LimitReader(file, maxSecretFileBytes+1))
	if err != nil {
		return out, fmt.Errorf("read all: %w", err)
	}

	return out, nil
}

func sanitizeSecretValue(path string, data []byte) (string, error) {
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
