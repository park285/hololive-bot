package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/kapu/hololive-stream-ingester/cmd/ops/internal/reportcli"
	opsapp "github.com/kapu/hololive-stream-ingester/internal/ops"
)

func main() {
	window := flag.Duration("window", 24*time.Hour, "lookback window for recent community/shorts delivery logs")
	observationRuntime := flag.String("observation-runtime", "", "runtime name for a specific observation window")
	observationCutover := flag.String("observation-cutover", "", "RFC3339 cutover timestamp for a specific observation window")
	limit := flag.Int("limit", 200, "maximum number of delivery log rows to return")
	format := flag.String("format", "markdown", "output format: markdown or json")
	flag.Parse()

	err := reportcli.RunOptionalObservationReport(
		reportcli.OptionalObservationParams{
			Runtime: *observationRuntime,
			Cutover: *observationCutover,
			Format:  *format,
		},
		reportcli.OptionalObservationCommand[
			opsapp.CommunityShortsDeliveryLogCollectOptions,
			opsapp.CommunityShortsDeliveryLogReport,
		]{
			BuildOptions: func(now time.Time, query reportcli.ObservationQuery, useObservationQuery bool) (opsapp.CommunityShortsDeliveryLogCollectOptions, error) {
				if !useObservationQuery && *window <= 0 {
					return opsapp.CommunityShortsDeliveryLogCollectOptions{}, fmt.Errorf("window must be greater than zero")
				}
				if *limit <= 0 {
					return opsapp.CommunityShortsDeliveryLogCollectOptions{}, fmt.Errorf("limit must be greater than zero")
				}
				options := opsapp.CommunityShortsDeliveryLogCollectOptions{
					ObservationRuntimeName: query.Runtime,
					Limit:                  *limit,
				}
				if useObservationQuery {
					options.ObservationBigBangCutoverAt = &query.CutoverAt
					return options, nil
				}
				since := now.Add(-*window)
				options.Since = &since
				return options, nil
			},
			Collect:            opsapp.CollectCommunityShortsDeliveryLogReport,
			RenderMarkdown:     opsapp.RenderCommunityShortsDeliveryLogMarkdown,
			LoadConfigError:    "Failed to load community/shorts delivery-log config",
			CollectError:       "Failed to collect community/shorts delivery logs",
			MarkdownWriteError: "Failed to write community/shorts delivery-log markdown",
			JSONWriteError:     "Failed to write community/shorts delivery-log JSON",
		},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
