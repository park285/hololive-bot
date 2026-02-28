package automaxprocs

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"go.uber.org/automaxprocs/maxprocs"
)

const DisableEnv = "AUTOMAXPROCS_DISABLE"

func Init(logger *slog.Logger) {
	if isDisabled() {
		if logger != nil {
			logger.Info("automaxprocs disabled by env", "env", DisableEnv)
		}
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

func isDisabled() bool {
	v := strings.TrimSpace(os.Getenv(DisableEnv))
	v = strings.ToLower(v)
	return v == "1" || v == "true" || v == "yes" || v == "y"
}
