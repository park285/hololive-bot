package reportcli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
)

type WindowParams struct {
	Window time.Duration
	Format string
}

type WindowCommand[Options any, Report any] struct {
	Stdout             io.Writer
	Stderr             io.Writer
	LoadConfig         func() (*config.Config, error)
	NewLogger          func(io.Writer) *slog.Logger
	Now                func() time.Time
	Timeout            time.Duration
	BuildOptions       func(time.Time, time.Duration) (Options, error)
	Collect            func(context.Context, *config.Config, *slog.Logger, time.Time, Options) (Report, error)
	RenderMarkdown     func(Report) string
	LoadConfigError    string
	CollectError       string
	MarkdownWriteError string
	JSONWriteError     string
}

func RunWindowReport[Options any, Report any](
	params WindowParams,
	command WindowCommand[Options, Report],
) error {
	if params.Window <= 0 {
		return errors.New("window must be greater than zero")
	}

	now := reportNow(command.Now)
	options, err := command.BuildOptions(now, params.Window)
	if err != nil {
		return err
	}

	cfg, err := loadReportConfig(command.LoadConfig)
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(command.LoadConfigError), err)
	}

	logger := newReportLogger(command.Stderr, command.NewLogger)
	ctx, cancel := context.WithTimeout(context.Background(), normalizeReportTimeout(command.Timeout))
	defer cancel()

	report, err := command.Collect(ctx, cfg, logger, now, options)
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(command.CollectError), err)
	}

	return writeFormattedReport(
		reportStdout(command.Stdout),
		params.Format,
		report,
		command.RenderMarkdown,
		strings.TrimSpace(command.MarkdownWriteError),
		strings.TrimSpace(command.JSONWriteError),
	)
}
