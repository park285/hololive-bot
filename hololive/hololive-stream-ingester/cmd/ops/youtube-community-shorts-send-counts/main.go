package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/kapu/hololive-stream-ingester/cmd/ops/internal/observationquery"
	"github.com/kapu/hololive-stream-ingester/cmd/ops/internal/reportcli"
	opsapp "github.com/kapu/hololive-stream-ingester/internal/ops"
)

func main() {
	window := flag.Duration("window", 24*time.Hour, "lookback window for recent community/shorts per-post send counts")
	observationRuntime := flag.String("observation-runtime", "", "runtime name for a specific observation window")
	observationCutover := flag.String("observation-cutover", "", "RFC3339 cutover timestamp for a specific observation window")
	format := flag.String("format", "markdown", "output format: markdown or json")
	flag.Parse()

	windowExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "window" {
			windowExplicit = true
		}
	})

	err := reportcli.RunOptionalObservationReport(
		reportcli.OptionalObservationParams{
			Runtime: *observationRuntime,
			Cutover: *observationCutover,
			Format:  *format,
		},
		reportcli.OptionalObservationCommand[
			opsapp.CommunityShortsSendCountCollectOptions,
			opsapp.CommunityShortsSendCountReport,
		]{
			BuildOptions: func(now time.Time, query reportcli.ObservationQuery, useObservationQuery bool) (opsapp.CommunityShortsSendCountCollectOptions, error) {
				if err := validateCommunityShortsSendCountCLIArgs(*window, windowExplicit, *observationRuntime, *observationCutover); err != nil {
					return opsapp.CommunityShortsSendCountCollectOptions{}, err
				}
				options := opsapp.CommunityShortsSendCountCollectOptions{
					ObservationRuntimeName: query.Runtime,
				}
				if useObservationQuery {
					options.ObservationBigBangCutoverAt = &query.CutoverAt
					return options, nil
				}
				since := now.Add(-*window)
				options.Since = &since
				return options, nil
			},
			Collect:            opsapp.CollectCommunityShortsSendCountReportWithOptions,
			RenderMarkdown:     opsapp.RenderCommunityShortsSendCountMarkdown,
			LoadConfigError:    "Failed to load community/shorts send-count config",
			CollectError:       "Failed to collect community/shorts send counts",
			MarkdownWriteError: "Failed to write community/shorts send-count markdown",
			JSONWriteError:     "Failed to write community/shorts send-count JSON",
		},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func validateCommunityShortsSendCountCLIArgs(
	window time.Duration,
	windowExplicit bool,
	observationRuntime string,
	observationCutover string,
) error {
	_, useObservationQuery, err := observationquery.ParseOptional(observationRuntime, observationCutover)
	if err != nil {
		return err
	}

	if !useObservationQuery {
		if window <= 0 {
			return errors.New("window must be greater than zero")
		}
		return nil
	}
	if windowExplicit {
		return errors.New("window and observation query flags are mutually exclusive")
	}
	return nil
}
