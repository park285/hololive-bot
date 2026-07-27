package communityshortscli

import (
	"fmt"
	"time"

	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/reports/deliverylogs"

	"github.com/kapu/hololive-youtube-producer/cmd/ops/internal/reportcli"
)

type deliveryLogsFlags struct {
	window *time.Duration
	limit  *int
	format *string
}

func runDeliveryLogsCommand(ctx commandContext, args []string) error {
	flags, err := parseDeliveryLogsFlags(ctx, args)
	if err != nil {
		return err
	}

	return reportcli.RunWindowReport(
		reportcli.WindowParams{Window: *flags.window, Format: *flags.format},
		deliveryLogsReportCommand(ctx, flags),
	)
}

func parseDeliveryLogsFlags(ctx commandContext, args []string) (deliveryLogsFlags, error) {
	fs := newFlagSet(ctx, "delivery-logs")
	flags := deliveryLogsFlags{
		window: fs.Duration("window", 24*time.Hour, "lookback window for recent community/shorts delivery logs"),
		limit:  fs.Int("limit", 200, "maximum number of delivery log rows to return"),
		format: fs.String("format", "markdown", "output format: markdown or json"),
	}
	if err := fs.Parse(args); err != nil {
		return deliveryLogsFlags{}, err
	}
	return flags, nil
}

func deliveryLogsReportCommand(ctx commandContext, flags deliveryLogsFlags) reportcli.WindowCommand[
	deliverylogs.CollectOptions,
	deliverylogs.Report,
] {
	return reportcli.WindowCommand[
		deliverylogs.CollectOptions,
		deliverylogs.Report,
	]{
		Stdout:             ctx.stdout,
		Stderr:             ctx.stderr,
		BuildOptions:       buildDeliveryLogsOptions(flags),
		Collect:            deliverylogs.Collect,
		RenderMarkdown:     deliverylogs.RenderMarkdown,
		LoadConfigError:    "Failed to load community/shorts delivery-log config",
		CollectError:       "Failed to collect community/shorts delivery logs",
		JSONWriteError:     "Failed to write community/shorts delivery-log JSON",
		MarkdownWriteError: "Failed to write community/shorts delivery-log markdown",
	}
}

func buildDeliveryLogsOptions(flags deliveryLogsFlags) func(
	time.Time,
	time.Duration,
) (deliverylogs.CollectOptions, error) {
	return func(now time.Time, window time.Duration) (deliverylogs.CollectOptions, error) {
		if window <= 0 {
			return deliverylogs.CollectOptions{}, fmt.Errorf("window must be greater than zero")
		}
		if *flags.limit <= 0 {
			return deliverylogs.CollectOptions{}, fmt.Errorf("limit must be greater than zero")
		}
		since := now.Add(-window)
		return deliverylogs.CollectOptions{
			Since: &since,
			Limit: *flags.limit,
		}, nil
	}
}
