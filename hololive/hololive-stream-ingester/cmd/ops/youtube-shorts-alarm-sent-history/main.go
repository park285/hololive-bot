package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kapu/hololive-stream-ingester/cmd/ops/internal/reportcli"
	opsapp "github.com/kapu/hololive-stream-ingester/internal/ops/communityshorts"
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
			opsapp.ShortsAlarmSentHistoryCollectOptions,
			opsapp.ShortsAlarmSentHistoryReport,
		]{
			BuildOptions: func(query reportcli.ObservationQuery) opsapp.ShortsAlarmSentHistoryCollectOptions {
				return opsapp.ShortsAlarmSentHistoryCollectOptions{
					ObservationRuntimeName:      query.Runtime,
					ObservationBigBangCutoverAt: &query.CutoverAt,
				}
			},
			Collect:            opsapp.CollectShortsAlarmSentHistoryReport,
			RenderMarkdown:     opsapp.RenderShortsAlarmSentHistoryMarkdown,
			LoadConfigError:    "Failed to load shorts alarm sent-history config",
			CollectError:       "Failed to collect shorts alarm sent history",
			MarkdownWriteError: "Failed to write shorts alarm sent-history markdown",
			JSONWriteError:     "Failed to write shorts alarm sent-history JSON",
		},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
