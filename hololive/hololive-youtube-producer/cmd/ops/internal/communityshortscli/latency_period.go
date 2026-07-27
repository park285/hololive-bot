package communityshortscli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts/reports/latencycause"
)

func runLatencyPeriodSummaryCommand(ctx commandContext, args []string) error {
	fs := newFlagSet(ctx, "latency-period-summary")
	var periods periodFlagValues
	fs.Var(&periods, "period", "period spec in label=duration form; repeatable, defaults to last_1h=1h,last_24h=24h,last_7d=168h")
	format := fs.String("format", "markdown", "output format: markdown or json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	specs, err := parseLatencyPeriodSpecs(periods)
	if err != nil {
		return fmt.Errorf("invalid period flag: %w", err)
	}

	appConfig, err := settings.LoadYouTubeProducerRuntime()
	if err != nil {
		return fmt.Errorf("failed to load community/shorts latency-period config: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(ctx.stderr, nil))
	now := time.Now().UTC()
	reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := latencycause.CollectPeriodReport(reqCtx, appConfig, logger, now, specs)
	if err != nil {
		return fmt.Errorf("failed to collect community/shorts latency period report: %w", err)
	}

	return writeLatencyPeriodReport(ctx, *format, report)
}

func writeLatencyPeriodReport(ctx commandContext, format string, report latencycause.PeriodReport) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "markdown":
		return writeLatencyPeriodMarkdown(ctx, report)
	case "json":
		return writeLatencyPeriodJSON(ctx, report)
	default:
		return fmt.Errorf("unsupported format %q (want markdown or json)", format)
	}
}

func writeLatencyPeriodMarkdown(ctx commandContext, report latencycause.PeriodReport) error {
	if _, err := fmt.Fprint(ctx.stdout, latencycause.RenderPeriodMarkdown(&report)); err != nil {
		return fmt.Errorf("failed to write community/shorts latency period markdown: %w", err)
	}
	return nil
}

func writeLatencyPeriodJSON(ctx commandContext, report latencycause.PeriodReport) error {
	encoder := json.NewEncoder(ctx.stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("failed to write community/shorts latency period JSON: %w", err)
	}
	return nil
}
