package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kapu/hololive-stream-ingester/cmd/ops/internal/reportcli"
	opsapp "github.com/kapu/hololive-stream-ingester/internal/ops"
)

func main() {
	observationRuntime := flag.String("observation-runtime", "", "runtime name for a specific observation window")
	observationCutover := flag.String("observation-cutover", "", "RFC3339 cutover timestamp for a specific observation window")
	format := flag.String("format", "markdown", "output format: markdown or json")
	flag.Parse()

	err := reportcli.RunRequiredObservationReport(
		reportcli.RequiredObservationParams{
			Runtime: *observationRuntime,
			Cutover: *observationCutover,
			Format:  *format,
		},
		reportcli.RequiredObservationCommand[
			opsapp.CommunityShortsAlarmSentHistoryDatasetCollectOptions,
			opsapp.CommunityShortsAlarmSentHistoryDatasetReport,
		]{
			BuildOptions: func(query reportcli.ObservationQuery) opsapp.CommunityShortsAlarmSentHistoryDatasetCollectOptions {
				return opsapp.CommunityShortsAlarmSentHistoryDatasetCollectOptions{
					ObservationRuntimeName:      query.Runtime,
					ObservationBigBangCutoverAt: &query.CutoverAt,
				}
			},
			Collect:            opsapp.CollectCommunityShortsAlarmSentHistoryDatasetReport,
			RenderMarkdown:     opsapp.RenderCommunityShortsAlarmSentHistoryDatasetMarkdown,
			LoadConfigError:    "Failed to load community/shorts alarm sent-history dataset config",
			CollectError:       "Failed to collect community/shorts alarm sent history dataset",
			MarkdownWriteError: "Failed to write community/shorts alarm sent-history dataset markdown",
			JSONWriteError:     "Failed to write community/shorts alarm sent-history dataset JSON",
		},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
