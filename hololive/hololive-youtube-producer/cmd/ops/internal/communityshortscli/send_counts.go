package communityshortscli

import (
	"errors"
	"time"

	"github.com/kapu/hololive-youtube-producer/cmd/ops/internal/reportcli"
	opsapp "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts"
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
	opsapp.CommunityShortsSendCountCollectOptions,
	opsapp.CommunityShortsSendCountReport,
] {
	return reportcli.WindowCommand[
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

func buildSendCountsOptions(_ sendCountsFlags) func(
	time.Time,
	time.Duration,
) (opsapp.CommunityShortsSendCountCollectOptions, error) {
	return func(now time.Time, window time.Duration) (opsapp.CommunityShortsSendCountCollectOptions, error) {
		if window <= 0 {
			return opsapp.CommunityShortsSendCountCollectOptions{}, errors.New("window must be greater than zero")
		}
		since := now.Add(-window)
		return opsapp.CommunityShortsSendCountCollectOptions{Since: &since}, nil
	}
}
