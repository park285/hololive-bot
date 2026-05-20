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

type testReport struct {
	Message string `json:"message"`
}

func TestRunRequiredObservationReport(t *testing.T) {
	t.Parallel()

	newCommand := func(stdout io.Writer) RequiredObservationCommand[string, testReport] {
		return RequiredObservationCommand[string, testReport]{
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
			BuildOptions: func(query ObservationQuery) string {
				return query.Runtime + "@" + query.CutoverAt.Format(time.RFC3339)
			},
			Collect: func(_ context.Context, _ *config.Config, _ *slog.Logger, _ time.Time, options string) (testReport, error) {
				return testReport{Message: options}, nil
			},
			RenderMarkdown: func(report testReport) string {
				return "markdown:" + report.Message
			},
			LoadConfigError:    "Failed to load test config",
			CollectError:       "Failed to collect test report",
			MarkdownWriteError: "Failed to write test markdown",
			JSONWriteError:     "Failed to write test JSON",
		}
	}

	t.Run("rejects incomplete observation query", func(t *testing.T) {
		var stdout bytes.Buffer
		err := RunRequiredObservationReport(
			RequiredObservationParams{Runtime: "youtube-producer", Cutover: "", Format: "markdown"},
			newCommand(&stdout),
		)
		if err == nil || err.Error() != "observation-runtime and observation-cutover must be provided together" {
			t.Fatalf("RunRequiredObservationReport() error = %v", err)
		}
	})

	t.Run("wraps config load errors", func(t *testing.T) {
		var stdout bytes.Buffer
		command := newCommand(&stdout)
		command.LoadConfig = func() (*config.Config, error) {
			return nil, errors.New("boom")
		}

		err := RunRequiredObservationReport(
			RequiredObservationParams{Runtime: "youtube-producer", Cutover: "2026-04-10T00:00:00Z", Format: "markdown"},
			command,
		)
		if err == nil || err.Error() != "Failed to load test config: boom" {
			t.Fatalf("RunRequiredObservationReport() error = %v", err)
		}
	})

	t.Run("writes markdown output", func(t *testing.T) {
		var stdout bytes.Buffer
		err := RunRequiredObservationReport(
			RequiredObservationParams{Runtime: " youtube-producer ", Cutover: "2026-04-10T00:00:00Z", Format: "markdown"},
			newCommand(&stdout),
		)
		if err != nil {
			t.Fatalf("RunRequiredObservationReport() error = %v", err)
		}
		if stdout.String() != "markdown:youtube-producer@2026-04-10T00:00:00Z" {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	t.Run("writes json output", func(t *testing.T) {
		var stdout bytes.Buffer
		err := RunRequiredObservationReport(
			RequiredObservationParams{Runtime: "youtube-producer", Cutover: "2026-04-10T00:00:00Z", Format: "json"},
			newCommand(&stdout),
		)
		if err != nil {
			t.Fatalf("RunRequiredObservationReport() error = %v", err)
		}
		want := "{\n  \"message\": \"youtube-producer@2026-04-10T00:00:00Z\"\n}\n"
		if stdout.String() != want {
			t.Fatalf("stdout = %q, want %q", stdout.String(), want)
		}
	})

	t.Run("wraps collect errors", func(t *testing.T) {
		var stdout bytes.Buffer
		command := newCommand(&stdout)
		command.Collect = func(context.Context, *config.Config, *slog.Logger, time.Time, string) (testReport, error) {
			return testReport{}, errors.New("collect boom")
		}

		err := RunRequiredObservationReport(
			RequiredObservationParams{Runtime: "youtube-producer", Cutover: "2026-04-10T00:00:00Z", Format: "markdown"},
			command,
		)
		if err == nil || err.Error() != "Failed to collect test report: collect boom" {
			t.Fatalf("RunRequiredObservationReport() error = %v", err)
		}
	})

	t.Run("rejects unsupported format", func(t *testing.T) {
		var stdout bytes.Buffer
		err := RunRequiredObservationReport(
			RequiredObservationParams{Runtime: "youtube-producer", Cutover: "2026-04-10T00:00:00Z", Format: "yaml"},
			newCommand(&stdout),
		)
		if err == nil || err.Error() != "unsupported format \"yaml\" (want markdown or json)" {
			t.Fatalf("RunRequiredObservationReport() error = %v", err)
		}
	})
}
