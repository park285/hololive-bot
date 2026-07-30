package communityshortscli

import (
	"errors"
	"time"

	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/reports/sendcounts"

	"github.com/kapu/hololive-youtube-producer/cmd/ops/internal/reportcli"
)

type sendCountsFlags struct {
	window *time.Duration
	format *string
}

func runSendCountsCommand(ctx commandContext, args []string) error {
	flags, err := parseSendCountsFlags(ctx, args)
	if err != nil {
		return err
	}

	return reportcli.RunWindowReport(
		reportcli.WindowParams{Window: *flags.window, Format: *flags.format},
		sendCountsReportCommand(ctx, flags),
	)
}

func parseSendCountsFlags(ctx commandContext, args []string) (sendCountsFlags, error) {
	fs := newFlagSet(ctx, "send-counts")
	flags := sendCountsFlags{
		window: fs.Duration("window", 24*time.Hour, "lookback window for recent community/shorts per-post send counts"),
		format: fs.String("format", "markdown", "output format: markdown or json"),
	}
	if err := fs.Parse(args); err != nil {
		return sendCountsFlags{}, err
	}
	return flags, nil
}

func sendCountsReportCommand(ctx commandContext, flags sendCountsFlags) reportcli.WindowCommand[
	sendcounts.CollectOptions,
	sendcounts.Report,
] {
	return reportcli.WindowCommand[
		sendcounts.CollectOptions,
		sendcounts.Report,
	]{
		Stdout:             ctx.stdout,
		Stderr:             ctx.stderr,
		BuildOptions:       buildSendCountsOptions(flags),
		Collect:            sendcounts.CollectWithOptions,
		RenderMarkdown:     sendcounts.RenderMarkdown,
		LoadConfigError:    "Failed to load community/shorts send-count config",
		CollectError:       "Failed to collect community/shorts send counts",
		MarkdownWriteError: "Failed to write community/shorts send-count markdown",
		JSONWriteError:     "Failed to write community/shorts send-count JSON",
	}
}

func buildSendCountsOptions(_ sendCountsFlags) func(
	time.Time,
	time.Duration,
) (sendcounts.CollectOptions, error) {
	return func(now time.Time, window time.Duration) (sendcounts.CollectOptions, error) {
		if window <= 0 {
			return sendcounts.CollectOptions{}, errors.New("window must be greater than zero")
		}
		since := now.Add(-window)
		return sendcounts.CollectOptions{Since: &since}, nil
	}
}
