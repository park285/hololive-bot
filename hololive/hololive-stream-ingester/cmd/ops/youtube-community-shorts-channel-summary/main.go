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
	window := flag.Duration("window", 24*time.Hour, "lookback window for community/shorts per-channel delivery summary")
	format := flag.String("format", "markdown", "output format: markdown or json")
	flag.Parse()

	err := reportcli.RunWindowReport(
		reportcli.WindowParams{Window: *window, Format: *format},
		reportcli.WindowCommand[time.Time, opsapp.CommunityShortsChannelSummaryReport]{
			BuildOptions: func(now time.Time, window time.Duration) (time.Time, error) {
				return now.Add(-window), nil
			},
			Collect:            opsapp.CollectCommunityShortsChannelSummaryReport,
			RenderMarkdown:     opsapp.RenderCommunityShortsChannelSummaryMarkdown,
			LoadConfigError:    "Failed to load community/shorts channel-summary config",
			CollectError:       "Failed to collect community/shorts channel summary",
			MarkdownWriteError: "Failed to write community/shorts channel-summary markdown",
			JSONWriteError:     "Failed to write community/shorts channel-summary JSON",
		},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
