package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/kapu/hololive-stream-ingester/cmd/ops/internal/observationquery"
	"github.com/kapu/hololive-stream-ingester/cmd/ops/internal/reportcli"
	opsapp "github.com/kapu/hololive-stream-ingester/internal/ops/communityshorts"
)

type sendCountsFlags struct {
	window             *time.Duration
	observationRuntime *string
	observationCutover *string
	format             *string
	windowExplicit     bool
}

func main() {
	flags := parseSendCountsFlags()

	err := reportcli.RunOptionalObservationReport(
		sendCountsReportParams(flags),
		sendCountsReportCommand(flags),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func parseSendCountsFlags() sendCountsFlags {
	flags := sendCountsFlags{
		window:             flag.Duration("window", 24*time.Hour, "lookback window for recent community/shorts per-post send counts"),
		observationRuntime: flag.String("observation-runtime", "", "runtime name for a specific observation window"),
		observationCutover: flag.String("observation-cutover", "", "RFC3339 cutover timestamp for a specific observation window"),
		format:             flag.String("format", "markdown", "output format: markdown or json"),
	}
	flag.Parse()
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "window" {
			flags.windowExplicit = true
		}
	})
	return flags
}

func sendCountsReportParams(flags sendCountsFlags) reportcli.OptionalObservationParams {
	return reportcli.OptionalObservationParams{
		Runtime: *flags.observationRuntime,
		Cutover: *flags.observationCutover,
		Format:  *flags.format,
	}
}

func sendCountsReportCommand(flags sendCountsFlags) reportcli.OptionalObservationCommand[
	opsapp.CommunityShortsSendCountCollectOptions,
	opsapp.CommunityShortsSendCountReport,
] {
	return reportcli.OptionalObservationCommand[
		opsapp.CommunityShortsSendCountCollectOptions,
		opsapp.CommunityShortsSendCountReport,
	]{
		BuildOptions:       buildSendCountsOptions(flags),
		Collect:            opsapp.CollectCommunityShortsSendCountReportWithOptions,
		RenderMarkdown:     opsapp.RenderCommunityShortsSendCountMarkdown,
		LoadConfigError:    "Failed to load community/shorts send-count config",
		CollectError:       "Failed to collect community/shorts send counts",
		MarkdownWriteError: "Failed to write community/shorts send-count markdown",
		JSONWriteError:     "Failed to write community/shorts send-count JSON",
	}
}

func buildSendCountsOptions(flags sendCountsFlags) func(
	time.Time,
	reportcli.ObservationQuery,
	bool,
) (opsapp.CommunityShortsSendCountCollectOptions, error) {
	return func(now time.Time, query reportcli.ObservationQuery, useObservationQuery bool) (opsapp.CommunityShortsSendCountCollectOptions, error) {
		return newSendCountsCollectOptions(flags, now, query, useObservationQuery)
	}
}

func newSendCountsCollectOptions(
	flags sendCountsFlags,
	now time.Time,
	query reportcli.ObservationQuery,
	useObservationQuery bool,
) (opsapp.CommunityShortsSendCountCollectOptions, error) {
	if err := validateCommunityShortsSendCountCLIArgs(
		*flags.window,
		flags.windowExplicit,
		*flags.observationRuntime,
		*flags.observationCutover,
	); err != nil {
		return opsapp.CommunityShortsSendCountCollectOptions{}, err
	}
	options := opsapp.CommunityShortsSendCountCollectOptions{
		ObservationRuntimeName: query.Runtime,
	}
	if useObservationQuery {
		options.ObservationBigBangCutoverAt = &query.CutoverAt
		return options, nil
	}
	since := now.Add(-*flags.window)
	options.Since = &since
	return options, nil
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
