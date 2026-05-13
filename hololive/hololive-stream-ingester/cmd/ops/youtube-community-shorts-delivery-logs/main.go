package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/kapu/hololive-stream-ingester/cmd/ops/internal/reportcli"
	opsapp "github.com/kapu/hololive-stream-ingester/internal/ops"
)

type deliveryLogsFlags struct {
	window             *time.Duration
	observationRuntime *string
	observationCutover *string
	limit              *int
	format             *string
}

func main() {
	flags := parseDeliveryLogsFlags()

	err := reportcli.RunOptionalObservationReport(deliveryLogsReportParams(flags), deliveryLogsReportCommand(flags))
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func parseDeliveryLogsFlags() deliveryLogsFlags {
	flags := deliveryLogsFlags{
		window:             flag.Duration("window", 24*time.Hour, "lookback window for recent community/shorts delivery logs"),
		observationRuntime: flag.String("observation-runtime", "", "runtime name for a specific observation window"),
		observationCutover: flag.String("observation-cutover", "", "RFC3339 cutover timestamp for a specific observation window"),
		limit:              flag.Int("limit", 200, "maximum number of delivery log rows to return"),
		format:             flag.String("format", "markdown", "output format: markdown or json"),
	}
	flag.Parse()
	return flags
}

func deliveryLogsReportParams(flags deliveryLogsFlags) reportcli.OptionalObservationParams {
	return reportcli.OptionalObservationParams{
		Runtime: *flags.observationRuntime,
		Cutover: *flags.observationCutover,
		Format:  *flags.format,
	}
}

func deliveryLogsReportCommand(flags deliveryLogsFlags) reportcli.OptionalObservationCommand[
	opsapp.CommunityShortsDeliveryLogCollectOptions,
	opsapp.CommunityShortsDeliveryLogReport,
] {
	return reportcli.OptionalObservationCommand[
		opsapp.CommunityShortsDeliveryLogCollectOptions,
		opsapp.CommunityShortsDeliveryLogReport,
	]{
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
