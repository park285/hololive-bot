package geminidecision

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxAPIKeyBytes = 256

// AI Studio가 새로 발급하는 인증 키는 표준 키(AIza…)와 달리 `AQ.` 접두사에 점이 들어간다.
var apiKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{20,256}$`)

// LoadAPIKey는 키 한 줄만 담은 파일을 읽는다. 오류에는 파일 내용을 넣지 않는다.
func LoadAPIKey(path string) (key string, retErr error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("gemini api key path must be absolute")
	}

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return "", fmt.Errorf("open gemini api key directory: %w", err)
	}

	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close gemini api key directory: %w", closeErr))
		}
	}()

	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return "", fmt.Errorf("open gemini api key: %w", err)
	}

	defer file.Close()

	raw, err := io.ReadAll(io.LimitReader(file, maxAPIKeyBytes+2))
	if err != nil {
		return "", errors.New("read gemini api key failed")
	}

	key = strings.TrimSpace(string(raw))
	if !apiKeyPattern.MatchString(key) {
		return "", errors.New("gemini api key file must hold only the key")
	}

	return key, nil
}
