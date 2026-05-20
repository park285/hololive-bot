package communityshortscli

import (
	"time"

	"github.com/kapu/hololive-youtube-producer/cmd/ops/internal/reportcli"
	opsapp "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts"
)

func runChannelSummaryCommand(ctx commandContext, args []string) error {
	fs := newFlagSet(ctx, "channel-summary")
	window := fs.Duration("window", 24*time.Hour, "lookback window for community/shorts per-channel delivery summary")
	format := fs.String("format", "markdown", "output format: markdown or json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	return reportcli.RunWindowReport(
		reportcli.WindowParams{Window: *window, Format: *format},
		reportcli.WindowCommand[time.Time, opsapp.CommunityShortsChannelSummaryReport]{
			Stdout: ctx.stdout,
			Stderr: ctx.stderr,
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
}

func runRouteReportCommand(ctx commandContext, args []string) error {
	fs := newFlagSet(ctx, "route-report")
	window := fs.Duration("window", 24*time.Hour, "lookback window for actual delivery path evidence")
	format := fs.String("format", "markdown", "output format: markdown or json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	return reportcli.RunWindowReport(
		reportcli.WindowParams{Window: *window, Format: *format},
		reportcli.WindowCommand[time.Time, opsapp.CommunityShortsRouteVerificationReport]{
			Stdout: ctx.stdout,
			Stderr: ctx.stderr,
			BuildOptions: func(now time.Time, window time.Duration) (time.Time, error) {
				return now.Add(-window), nil
			},
			Collect:            opsapp.CollectCommunityShortsRouteVerificationReport,
			RenderMarkdown:     opsapp.RenderCommunityShortsRouteVerificationMarkdown,
			LoadConfigError:    "Failed to load community/shorts route verification config",
			CollectError:       "Failed to collect community/shorts route verification report",
			MarkdownWriteError: "Failed to write community/shorts route verification report",
			JSONWriteError:     "Failed to write community/shorts route verification JSON",
		},
	)
}
