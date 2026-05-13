package communityshortscli

import (
	"errors"
	"flag"
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

func runSendCountsCommand(ctx commandContext, args []string) error {
	flags, err := parseSendCountsFlags(ctx, args)
	if err != nil {
		return err
	}

	return reportcli.RunOptionalObservationReport(
		sendCountsReportParams(flags),
		sendCountsReportCommand(ctx, flags),
	)
}

func parseSendCountsFlags(ctx commandContext, args []string) (sendCountsFlags, error) {
	fs := newFlagSet(ctx, "send-counts")
	flags := sendCountsFlags{
		window:             fs.Duration("window", 24*time.Hour, "lookback window for recent community/shorts per-post send counts"),
		observationRuntime: fs.String("observation-runtime", "", "runtime name for a specific observation window"),
		observationCutover: fs.String("observation-cutover", "", "RFC3339 cutover timestamp for a specific observation window"),
		format:             fs.String("format", "markdown", "output format: markdown or json"),
	}
	if err := fs.Parse(args); err != nil {
		return sendCountsFlags{}, err
	}
	flags.windowExplicit = flagSetContains(fs, "window")
	return flags, nil
}

func flagSetContains(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		found = found || f.Name == name
	})
	return found
}

func sendCountsReportParams(flags sendCountsFlags) reportcli.OptionalObservationParams {
	return reportcli.OptionalObservationParams{
		Runtime: *flags.observationRuntime,
		Cutover: *flags.observationCutover,
		Format:  *flags.format,
	}
}

func sendCountsReportCommand(ctx commandContext, flags sendCountsFlags) reportcli.OptionalObservationCommand[
	opsapp.CommunityShortsSendCountCollectOptions,
	opsapp.CommunityShortsSendCountReport,
] {
	return reportcli.OptionalObservationCommand[
		opsapp.CommunityShortsSendCountCollectOptions,
		opsapp.CommunityShortsSendCountReport,
	]{
		Stdout:             ctx.stdout,
		Stderr:             ctx.stderr,
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
