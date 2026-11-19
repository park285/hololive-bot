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

	slog.SetDefault(logger)

	return logger, closer, nil
}
