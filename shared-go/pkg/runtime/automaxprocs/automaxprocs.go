package automaxprocs

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"go.uber.org/automaxprocs/maxprocs"
)

const (
	DisableEnv = "AUTOMAXPROCS_DISABLE"
	ForceEnv   = "AUTOMAXPROCS_FORCE"
)

func Init(logger *slog.Logger) {
	if isTruthy(os.Getenv(DisableEnv)) {
		logInfo(logger, "automaxprocs disabled by env", "env", DisableEnv)
		return
	}
	if !shouldRunAutomaxprocs() {
		logInfo(logger, "automaxprocs skipped; Go 1.25+ runtime handles container-aware GOMAXPROCS", "force_env", ForceEnv)
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	_, err := maxprocs.Set(maxprocs.Logger(func(format string, args ...any) {
		logger.Info(fmt.Sprintf(format, args...))
	}))
	if err != nil {
		logger.Warn("automaxprocs set failed", "err", err)
	}
}

func shouldRunAutomaxprocs() bool {
	return !isTruthy(os.Getenv(DisableEnv)) && isTruthy(os.Getenv(ForceEnv))
}

func isTruthy(v string) bool {
	v = strings.TrimSpace(v)
	v = strings.ToLower(v)
	return v == "1" || v == "true" || v == "yes" || v == "y"
}

func logInfo(logger *slog.Logger, msg string, fields ...any) {
	if logger != nil {
		logger.Info(msg, fields...)
	}
}
