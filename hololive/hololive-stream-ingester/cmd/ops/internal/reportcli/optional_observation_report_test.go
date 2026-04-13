package reportcli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
)

func TestRunOptionalObservationReport(t *testing.T) {
	t.Parallel()

	newCommand := func(stdout io.Writer) OptionalObservationCommand[string, testReport] {
		return OptionalObservationCommand[string, testReport]{
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
			BuildOptions: func(_ time.Time, query ObservationQuery, useObservationQuery bool) (string, error) {
				if useObservationQuery {
					return query.Runtime + "@" + query.CutoverAt.Format(time.RFC3339), nil
				}
				return "recent-window", nil
			},
			Collect: func(_ context.Context, _ *config.Config, _ *slog.Logger, _ time.Time, options string) (testReport, error) {
				return testReport{Message: options}, nil
			},
			RenderMarkdown:     func(report testReport) string { return "markdown:" + report.Message },
			LoadConfigError:    "Failed to load test config",
			CollectError:       "Failed to collect test report",
			MarkdownWriteError: "Failed to write test markdown",
			JSONWriteError:     "Failed to write test JSON",
		}
	}

	t.Run("rejects partial observation query", func(t *testing.T) {
		var stdout bytes.Buffer
		err := RunOptionalObservationReport(
			OptionalObservationParams{Runtime: "youtube-scraper", Cutover: "", Format: "markdown"},
			newCommand(&stdout),
		)
		if err == nil || err.Error() != "observation-runtime and observation-cutover must be provided together" {
			t.Fatalf("RunOptionalObservationReport() error = %v", err)
		}
	})

	t.Run("supports recent mode", func(t *testing.T) {
		var stdout bytes.Buffer
		err := RunOptionalObservationReport(
			OptionalObservationParams{Format: "markdown"},
			newCommand(&stdout),
		)
		if err != nil {
			t.Fatalf("RunOptionalObservationReport() error = %v", err)
		}
		if stdout.String() != "markdown:recent-window" {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	t.Run("supports observation mode", func(t *testing.T) {
		var stdout bytes.Buffer
		err := RunOptionalObservationReport(
			OptionalObservationParams{Runtime: "youtube-scraper", Cutover: "2026-04-10T00:00:00Z", Format: "json"},
			newCommand(&stdout),
		)
		if err != nil {
			t.Fatalf("RunOptionalObservationReport() error = %v", err)
		}
		want := "{\n  \"message\": \"youtube-scraper@2026-04-10T00:00:00Z\"\n}\n"
		if stdout.String() != want {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	})

	t.Run("returns option build errors", func(t *testing.T) {
		var stdout bytes.Buffer
		command := newCommand(&stdout)
		command.BuildOptions = func(time.Time, ObservationQuery, bool) (string, error) {
			return "", errors.New("bad options")
		}

		err := RunOptionalObservationReport(OptionalObservationParams{Format: "markdown"}, command)
		if err == nil || err.Error() != "bad options" {
			t.Fatalf("RunOptionalObservationReport() error = %v", err)
		}
	})
}
