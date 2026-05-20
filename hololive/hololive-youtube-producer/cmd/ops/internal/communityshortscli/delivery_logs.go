package communityshortscli

import (
	"fmt"
	"time"

	"github.com/kapu/hololive-youtube-producer/cmd/ops/internal/reportcli"
	opsapp "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts"
)

type deliveryLogsFlags struct {
	window             *time.Duration
	observationRuntime *string
	observationCutover *string
	limit              *int
	format             *string
}

func runDeliveryLogsCommand(ctx commandContext, args []string) error {
	flags, err := parseDeliveryLogsFlags(ctx, args)
	if err != nil {
		return err
	}

	return reportcli.RunOptionalObservationReport(deliveryLogsReportParams(flags), deliveryLogsReportCommand(ctx, flags))
}

func parseDeliveryLogsFlags(ctx commandContext, args []string) (deliveryLogsFlags, error) {
	fs := newFlagSet(ctx, "delivery-logs")
	flags := deliveryLogsFlags{
		window:             fs.Duration("window", 24*time.Hour, "lookback window for recent community/shorts delivery logs"),
		observationRuntime: fs.String("observation-runtime", "", "runtime name for a specific observation window"),
		observationCutover: fs.String("observation-cutover", "", "RFC3339 cutover timestamp for a specific observation window"),
		limit:              fs.Int("limit", 200, "maximum number of delivery log rows to return"),
		format:             fs.String("format", "markdown", "output format: markdown or json"),
	}
	if err := fs.Parse(args); err != nil {
		return deliveryLogsFlags{}, err
	}
	return flags, nil
}

func deliveryLogsReportParams(flags deliveryLogsFlags) reportcli.OptionalObservationParams {
	return reportcli.OptionalObservationParams{
		Runtime: *flags.observationRuntime,
		Cutover: *flags.observationCutover,
		Format:  *flags.format,
	}
}

func deliveryLogsReportCommand(ctx commandContext, flags deliveryLogsFlags) reportcli.OptionalObservationCommand[
	opsapp.CommunityShortsDeliveryLogCollectOptions,
	opsapp.CommunityShortsDeliveryLogReport,
] {
	return reportcli.OptionalObservationCommand[
		opsapp.CommunityShortsDeliveryLogCollectOptions,
		opsapp.CommunityShortsDeliveryLogReport,
	]{
		Stdout:             ctx.stdout,
		Stderr:             ctx.stderr,
		BuildOptions:       buildDeliveryLogsOptions(flags),
		Collect:            opsapp.CollectCommunityShortsDeliveryLogReport,
		RenderMarkdown:     opsapp.RenderCommunityShortsDeliveryLogMarkdown,
		LoadConfigError:    "Failed to load community/shorts delivery-log config",
		CollectError:       "Failed to collect community/shorts delivery logs",
		JSONWriteError:     "Failed to write community/shorts delivery-log JSON",
		MarkdownWriteError: "Failed to write community/shorts delivery-log markdown",
	}
}

func buildDeliveryLogsOptions(flags deliveryLogsFlags) func(
	time.Time,
	reportcli.ObservationQuery,
	bool,
) (opsapp.CommunityShortsDeliveryLogCollectOptions, error) {
	return func(now time.Time, query reportcli.ObservationQuery, useObservationQuery bool) (opsapp.CommunityShortsDeliveryLogCollectOptions, error) {
		return newDeliveryLogsCollectOptions(flags, now, query, useObservationQuery)
	}
}

func newDeliveryLogsCollectOptions(
	flags deliveryLogsFlags,
	now time.Time,
	query reportcli.ObservationQuery,
	useObservationQuery bool,
) (opsapp.CommunityShortsDeliveryLogCollectOptions, error) {
	if !useObservationQuery && *flags.window <= 0 {
		return opsapp.CommunityShortsDeliveryLogCollectOptions{}, fmt.Errorf("window must be greater than zero")
	}
	if *flags.limit <= 0 {
		return opsapp.CommunityShortsDeliveryLogCollectOptions{}, fmt.Errorf("limit must be greater than zero")
	}
	options := opsapp.CommunityShortsDeliveryLogCollectOptions{
		ObservationRuntimeName: query.Runtime,
		Limit:                  *flags.limit,
	}
	if useObservationQuery {
		options.ObservationBigBangCutoverAt = &query.CutoverAt
		return options, nil
	}
	since := now.Add(-*flags.window)
	options.Since = &since
	return options, nil
}
