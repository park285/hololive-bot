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
			opsapp.CommunityShortsSendStateCollectOptions,
			opsapp.CommunityShortsSendStateReport,
		]{
			BuildOptions: func(query reportcli.ObservationQuery) opsapp.CommunityShortsSendStateCollectOptions {
				return opsapp.CommunityShortsSendStateCollectOptions{
					ObservationRuntimeName:      query.Runtime,
					ObservationBigBangCutoverAt: &query.CutoverAt,
				}
			},
			Collect:            opsapp.CollectCommunityShortsSendStateReport,
			RenderMarkdown:     opsapp.RenderCommunityShortsSendStateMarkdown,
			LoadConfigError:    "Failed to load community/shorts send-state config",
			CollectError:       "Failed to collect community/shorts send state report",
			MarkdownWriteError: "Failed to write community/shorts send-state markdown",
			JSONWriteError:     "Failed to write community/shorts send-state JSON",
		},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
