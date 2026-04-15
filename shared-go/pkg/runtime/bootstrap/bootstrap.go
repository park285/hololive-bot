package bootstrap

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/kapu/hololive-shared/pkg/health"

	sharedlogging "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/runtime/automaxprocs"
)

type runtime interface {
	Run()
	Close()
}

type Options[Config any, Runtime runtime] struct {
	Version                string
	Initialize             func(version string)
	LoadConfig             func() (Config, error)
	LoadConfigErrorMessage string
	NewLogger              func(cfg Config) (*slog.Logger, error)
	LoggerConfig           func(cfg Config) sharedlogging.Config
	LoggerFileName         string
	LoggerLevel            func(cfg Config) string
	StartupMessage         string
	StartupFields          func(cfg Config) []any
	BuildTimeout           time.Duration
	BuildRuntime           func(ctx context.Context, cfg Config, logger *slog.Logger) (Runtime, error)
	BuildErrorMessage      string
	Stderr                 io.Writer
}

func Run[Config any, Runtime runtime](opts Options[Config, Runtime]) int {
	initialize := opts.Initialize
	if initialize == nil {
		initialize = func(version string) {
			automaxprocs.Init(nil)
			health.Init(version)
		}
	}
	initialize(opts.Version)

	stderr := opts.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	cfg, err := opts.LoadConfig()
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "%s: %v\n", fallback(opts.LoadConfigErrorMessage, "Failed to load config"), err); writeErr != nil {
			return 1
		}
		return 1
	}

	logger, err := newLogger(opts, cfg)
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "Failed to initialize logger: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}

	if message := opts.StartupMessage; message != "" {
		logger.Info(message, startupFields(opts, cfg)...)
	}

	buildCtx, buildCancel := context.WithTimeout(context.Background(), opts.BuildTimeout)
	defer buildCancel()

	rt, err := opts.BuildRuntime(buildCtx, cfg, logger)
	if err != nil {
		logger.Error(fallback(opts.BuildErrorMessage, "Failed to build runtime"), slog.Any("error", err))
		return 1
	}
	defer rt.Close()

	rt.Run()
	return 0
}

func newLogger[Config any, Runtime runtime](opts Options[Config, Runtime], cfg Config) (*slog.Logger, error) {
	if opts.NewLogger != nil {
		return opts.NewLogger(cfg)
	}

	logCfg := sharedlogging.Config{}
	if opts.LoggerConfig != nil {
		logCfg = opts.LoggerConfig(cfg)
	}

	level := ""
	if opts.LoggerLevel != nil {
		level = opts.LoggerLevel(cfg)
	}

	return sharedlogging.EnableFileLoggingWithLevel(logCfg, opts.LoggerFileName, level)
}

func startupFields[Config any, Runtime runtime](opts Options[Config, Runtime], cfg Config) []any {
	fields := []any{
		slog.String("version", opts.Version),
	}

	if opts.LoggerLevel != nil {
		fields = append(fields, slog.String("log_level", opts.LoggerLevel(cfg)))
	}

	if opts.StartupFields == nil {
		return fields
	}

	return append(fields, opts.StartupFields(cfg)...)
}

func fallback(value, def string) string {
	if value == "" {
		return def
	}
	return value
}
