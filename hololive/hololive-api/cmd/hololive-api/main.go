package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"
	"github.com/park285/shared-go/v2/pkg/runtime/automaxprocs"

	"github.com/kapu/hololive-api/internal/fxapp"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/health"
)

var Version = "dev"

func main() {
	if handled, exitCode := runWorkerProfileCheck(os.Args[1:], os.Stderr, func() error {
		if _, err := settings.LoadAPIWorkerProfile(); err != nil {
			return fmt.Errorf("load api worker profile: %w", err)
		}

		return nil
	}); handled {
		os.Exit(exitCode)
	}

	if handled, exitCode := runConfigCheck(os.Args[1:], os.Stderr, func() error {
		if _, err := settings.LoadHololiveAPIRuntime(); err != nil {
			return fmt.Errorf("load hololive api runtime: %w", err)
		}

		return nil
	}); handled {
		os.Exit(exitCode)
	}

	var logCloser io.Closer

	code := runHololiveAPI(func(closer io.Closer) { logCloser = closer })

	if logCloser != nil {
		if err := logCloser.Close(); err != nil {
			slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("log closer close failed", slog.Any("error", err))
		}
	}

	os.Exit(code)
}

func runHololiveAPI(setLogCloser func(io.Closer)) int {
	return runHololiveAPIWithDependencies(setLogCloser, productionStartupDependencies())
}

type hololiveAPIApplication interface {
	Run(*slog.Logger) int
}

type loggerResult struct {
	logger *slog.Logger
	closer io.Closer
}

type startupDependencies struct {
	initialize     func(string)
	loadConfig     func() (*settings.HololiveAPIConfig, error)
	newLogger      func(*settings.HololiveAPIConfig) (loggerResult, error)
	newApplication func(context.Context, *settings.HololiveAPIConfig, *slog.Logger, string) (hololiveAPIApplication, error)
	stderr         io.Writer
}

func productionStartupDependencies() startupDependencies {
	return startupDependencies{
		initialize: func(version string) {
			automaxprocs.Init(nil)
			health.Init(version)
		},
		loadConfig: settings.LoadHololiveAPIRuntime,
		newLogger:  newHololiveAPILogger,
		newApplication: func(
			ctx context.Context,
			config *settings.HololiveAPIConfig,
			logger *slog.Logger,
			version string,
		) (hololiveAPIApplication, error) {
			return fxapp.New(ctx, config, logger, version)
		},
		stderr: os.Stderr,
	}
}

func runHololiveAPIWithDependencies(setLogCloser func(io.Closer), dependencies startupDependencies) int {
	dependencies.initialize(Version)

	config, err := dependencies.loadConfig()
	if err != nil {
		return printStartupError(dependencies.stderr, "Failed to load hololive-api config", err)
	}

	logOutput, err := dependencies.newLogger(config)

	if setLogCloser != nil {
		setLogCloser(logOutput.closer)
	}

	if err != nil {
		return printStartupError(dependencies.stderr, "Failed to initialize logger", err)
	}

	if logOutput.logger == nil {
		return printStartupError(dependencies.stderr, "Failed to initialize logger", errors.New("logger is nil"))
	}

	logger := logOutput.logger
	slog.SetDefault(logger)
	logger.Info(
		"Hololive unified API starting...",
		slog.String("version", Version),
		slog.String("log_level", config.Logging.Level),
		slog.Int("bot_port", config.Bot.Server.Port),
		slog.Int("admin_port", config.Admin.Server.Port),
		slog.Int("llm_port", config.LLM.Server.Port),
	)

	buildCtx, buildCancel := context.WithTimeout(context.Background(), constants.AppTimeout.Build)
	defer buildCancel()

	application, err := dependencies.newApplication(buildCtx, config, logger, Version)
	if err != nil {
		logger.Error(
			"Failed to assemble hololive-api runtime",
			slog.String("error", sharedlogging.RedactDiagnostic(err.Error())),
		)

		return 1
	}

	return application.Run(logger)
}

func newHololiveAPILogger(config *settings.HololiveAPIConfig) (loggerResult, error) {
	logger, closer, err := sharedlogging.EnableFileLoggingWithOptions(sharedlogging.Config{
		Level:      config.Logging.Level,
		Dir:        config.Logging.Dir,
		MaxSizeMB:  config.Logging.MaxSizeMB,
		MaxBackups: config.Logging.MaxBackups,
		MaxAgeDays: config.Logging.MaxAgeDays,
		Compress:   config.Logging.Compress,
	}, "hololive-api.log", sharedlogging.Options{AsyncStdout: true})
	result := loggerResult{logger: logger, closer: closer}

	if err != nil {
		return result, fmt.Errorf("enable file logging: %w", err)
	}

	return result, nil
}

func printStartupError(stderr io.Writer, message string, err error) int {
	if stderr == nil {
		stderr = os.Stderr
	}

	safeError := "unknown error"

	if err != nil {
		safeError = sharedlogging.RedactDiagnostic(err.Error())
	}

	if _, writeErr := fmt.Fprintf(
		stderr,
		"%s: %s\n",
		sharedlogging.RedactDiagnostic(message),
		safeError,
	); writeErr != nil {
		return 1
	}

	return 1
}

func runWorkerProfileCheck(args []string, stderr io.Writer, load func() error) (handled bool, exitCode int) {
	if len(args) != 1 || args[0] != "--check-worker-profile" {
		return false, 0
	}

	if err := load(); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Failed to load hololive-api worker profile: %v\n", err); writeErr != nil {
			return true, 1
		}

		return true, 1
	}

	if _, err := fmt.Fprintln(stderr, "hololive-api worker profile valid"); err != nil {
		return true, 1
	}

	return true, 0
}

func runConfigCheck(args []string, stderr io.Writer, load func() error) (handled bool, exitCode int) {
	if len(args) != 1 || args[0] != "--check-config" {
		return false, 0
	}

	if err := load(); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Failed to load hololive-api config: %v\n", err); writeErr != nil {
			return true, 1
		}

		return true, 1
	}

	if _, err := fmt.Fprintln(stderr, "hololive-api config valid"); err != nil {
		return true, 1
	}

	return true, 0
}
