// Package entrypoint는 hololive runtime main 패키지가 함께 쓰는 진입점 처리를 소유한다.
package entrypoint

import (
	"fmt"
	"io"
	"log/slog"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/privacylog"
)

const checkWorkerProfileFlag = "--check-worker-profile"

// RunWorkerProfileCheck는 args가 --check-worker-profile 하나일 때만 load를 실행해 결과를 stderr에 쓰고 handled=true를 돌려준다.
func RunWorkerProfileCheck(args []string, stderr io.Writer, runtimeName string, load func() error) (handled bool, exitCode int) {
	if len(args) != 1 || args[0] != checkWorkerProfileFlag {
		return false, 0
	}

	if err := load(); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Failed to load %s worker profile: %v\n", runtimeName, err); writeErr != nil {
			return true, 1
		}

		return true, 1
	}

	if _, err := fmt.Fprintf(stderr, "%s worker profile valid\n", runtimeName); err != nil {
		return true, 1
	}

	return true, 0
}

// NewFileLogger는 fileName 로그 파일과 비동기 stdout에 privacy 키를 가리고 쓰는 logger를 열어 slog 기본 logger로도 지정한다.
func NewFileLogger(config settings.LoggingConfig, fileName string) (*slog.Logger, io.Closer, error) {
	logger, closer, err := sharedlogging.EnableFileLoggingWithOptions(sharedlogging.Config{
		Level:      config.Level,
		Dir:        config.Dir,
		MaxSizeMB:  config.MaxSizeMB,
		MaxBackups: config.MaxBackups,
		MaxAgeDays: config.MaxAgeDays,
		Compress:   config.Compress,
	}, fileName, sharedlogging.Options{AsyncStdout: true, PrivacyKeys: privacylog.LoggerPrivacyKeys()})
	if err != nil {
		return nil, nil, fmt.Errorf("enable file logging: %w", err)
	}

	// slog.Default()로 기록하는 의존성도 같은 JSON 파일과 마스킹 경로로 보낸다.
	slog.SetDefault(logger)

	return logger, closer, nil
}
