package reportcli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-stream-ingester/cmd/ops/internal/observationquery"
)

type ObservationQuery = observationquery.Query

type RequiredObservationParams struct {
	Runtime string
	Cutover string
	Format  string
}

type RequiredObservationCommand[Options any, Report any] struct {
	Stdout             io.Writer
	Stderr             io.Writer
	LoadConfig         func() (*config.Config, error)
	NewLogger          func(io.Writer) *slog.Logger
	Now                func() time.Time
	Timeout            time.Duration
	BuildOptions       func(ObservationQuery) Options
	Collect            func(context.Context, *config.Config, *slog.Logger, time.Time, Options) (Report, error)
	RenderMarkdown     func(Report) string
	LoadConfigError    string
	CollectError       string
	MarkdownWriteError string
	JSONWriteError     string
}

func RunRequiredObservationReport[Options any, Report any](
	params RequiredObservationParams,
	command RequiredObservationCommand[Options, Report],
) error {
	query, err := observationquery.ParseRequired(params.Runtime, params.Cutover)
	if err != nil {
		return err
	}

	cfg, err := loadReportConfig(command.LoadConfig)
	if err != nil {
		return fmt.Errorf("%s: %w", strings.TrimSpace(command.LoadConfigError), err)
	}

	logger := newReportLogger(command.Stderr, command.NewLogger)
	now := reportNow(command.Now)
	timeout := normalizeReportTimeout(command.Timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	report, err := command.Collect(ctx, cfg, logger, now, command.BuildOptions(query))
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

func loadReportConfig(loadConfig func() (*config.Config, error)) (*config.Config, error) {
	if loadConfig == nil {
		loadConfig = config.Load
	}
	return loadConfig()
}

func newReportLogger(stderr io.Writer, newLogger func(io.Writer) *slog.Logger) *slog.Logger {
	if stderr == nil {
		stderr = os.Stderr
	}
	if newLogger == nil {
		newLogger = func(w io.Writer) *slog.Logger {
			return slog.New(slog.NewTextHandler(w, nil))
		}
	}
	return newLogger(stderr)
}

func reportNow(nowFn func() time.Time) time.Time {
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	return nowFn().UTC()
}

func normalizeReportTimeout(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return 2 * time.Minute
	}
	return timeout
}

func reportStdout(stdout io.Writer) io.Writer {
	if stdout == nil {
		return os.Stdout
	}
	return stdout
}

func writeFormattedReport[Report any](
	stdout io.Writer,
	format string,
	report Report,
	renderMarkdown func(Report) string,
	markdownWriteError string,
	jsonWriteError string,
) error {
	if strings.TrimSpace(format) == "" {
		format = "markdown"
	}
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "markdown":
		return writeMarkdownReport(stdout, report, renderMarkdown, markdownWriteError)
	case "json":
		return writeJSONReport(stdout, report, jsonWriteError)
	default:
		return fmt.Errorf("unsupported format %q (want markdown or json)", format)
	}
}

func writeMarkdownReport[Report any](
	stdout io.Writer,
	report Report,
	renderMarkdown func(Report) string,
	markdownWriteError string,
) error {
	if _, err := fmt.Fprint(stdout, renderMarkdown(report)); err != nil {
		return fmt.Errorf("%s: %w", markdownWriteError, err)
	}
	return nil
}

func writeJSONReport[Report any](stdout io.Writer, report Report, jsonWriteError string) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("%s: %w", jsonWriteError, err)
	}
	return nil
}
