package reportcli

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
)

func TestRunWindowReport(t *testing.T) {
	t.Parallel()

	newCommand := func(stdout io.Writer) WindowCommand[string, testReport] {
		return WindowCommand[string, testReport]{
			Stdout: stdout,
			LoadConfig: func() (*config.Config, error) {
				return &config.Config{}, nil
			},
			NewLogger: func(io.Writer) *slog.Logger {
				return slog.New(slog.NewTextHandler(io.Discard, nil))
			},
			Now: func() time.Time {
				return time.Date(2026, 4, 13, 7, 0, 0, 0, time.UTC)
			},
			BuildOptions: func(now time.Time, window time.Duration) (string, error) {
				return now.Add(-window).Format(time.RFC3339), nil
			},
			Collect: func(_ context.Context, _ *config.Config, _ *slog.Logger, _ time.Time, options string) (testReport, error) {
				return testReport{Message: options}, nil
			},
			RenderMarkdown:     func(report *testReport) string { return "markdown:" + report.Message },
			LoadConfigError:    "Failed to load test config",
			CollectError:       "Failed to collect test report",
			MarkdownWriteError: "Failed to write test markdown",
			JSONWriteError:     "Failed to write test JSON",
		}
	}

	t.Run("rejects non-positive window", func(t *testing.T) {
		var stdout bytes.Buffer
		err := RunWindowReport(WindowParams{Window: 0, Format: "markdown"}, newCommand(&stdout))
		if err == nil || err.Error() != "window must be greater than zero" {
			t.Fatalf("RunWindowReport() error = %v", err)
		}
	})

	t.Run("defaults empty format to markdown", func(t *testing.T) {
		var stdout bytes.Buffer
		err := RunWindowReport(WindowParams{Window: time.Hour}, newCommand(&stdout))
		if err != nil {
			t.Fatalf("RunWindowReport() error = %v", err)
		}
		if stdout.String() != "markdown:2026-04-13T06:00:00Z" {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})
}
