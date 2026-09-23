package xspaces

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"unicode"
)

// LoginConfig는 운영자가 별도 보호 파일로 제공한 전용 계정입니다. 로그나 응답으로 출력하지 않습니다.
type LoginConfig struct {
	Revision int64  `json:"revision"`
	Username string `json:"username"`
	Password string `json:"password"`
}

var loginUsername = regexp.MustCompile(`^[a-zA-Z0-9_]{1,15}$`)

// LoadLoginConfig는 private regular file만 읽고 크기·세대·입력 형식을 검증합니다.
// 파일 미설정은 비활성이며 기존 세션을 변경하지 않습니다.
func LoadLoginConfig(path string) (*LoginConfig, error) {
	raw, err := readLoginFile(path)
	if err != nil {
		return nil, err
	}
	defer clear(raw)

	var config LoginConfig

	if err := jsonv2.Unmarshal(raw, &config, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, errors.New("invalid X login configuration")
	}

	if config.Revision <= 0 || !loginUsername.MatchString(config.Username) || len(config.Password) == 0 || len(config.Password) > 1024 {
		return nil, errors.New("invalid X login configuration")
	}

	for _, r := range config.Password {
		if unicode.IsControl(r) {
			return nil, errors.New("invalid X login configuration")
		}
	}

	return &config, nil
}

func readLoginFile(path string) (_ []byte, retErr error) {
	if path == "" {
		return nil, ErrDisabled
	}

	if !filepath.IsAbs(path) {
		return nil, errors.New("x login file requires absolute path")
	}

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, errors.New("open X login directory failed")
	}

	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			retErr = errors.Join(retErr, errors.New("close X login directory failed"))
		}
	}()

	before, err := root.Lstat(filepath.Base(path))
	if err != nil || !before.Mode().IsRegular() {
		return nil, errors.New("x login file must be regular")
	}

	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return nil, errors.New("open X login file failed")
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || !os.SameFile(before, info) || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 8192 {
		return nil, errors.New("x login file must be private and bounded")
	}

	raw, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil || len(raw) > 8192 {
		return nil, errors.New("read X login file failed")
	}

	return raw, nil
}
