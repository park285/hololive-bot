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
	window := flag.Duration("window", 24*time.Hour, "lookback window for actual delivery path evidence")
	flag.Parse()

	err := reportcli.RunWindowReport(
		reportcli.WindowParams{Window: *window},
		reportcli.WindowCommand[time.Time, opsapp.CommunityShortsRouteVerificationReport]{
			BuildOptions: func(now time.Time, window time.Duration) (time.Time, error) {
				return now.Add(-window), nil
			},
			Collect:            opsapp.CollectCommunityShortsRouteVerificationReport,
			RenderMarkdown:     opsapp.RenderCommunityShortsRouteVerificationMarkdown,
			LoadConfigError:    "Failed to load community/shorts route verification config",
			CollectError:       "Failed to collect community/shorts route verification report",
			MarkdownWriteError: "Failed to write community/shorts route verification report",
		},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
