package communityshortscli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	opsapp "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts"
)

func runLatencyCauseCommand(ctx commandContext, args []string) error {
	fs := newFlagSet(ctx, "latency-cause-report")
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

	appConfig, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load community/shorts latency-cause config: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(ctx.stderr, nil))
	now := time.Now().UTC()
	reqCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := opsapp.CollectCommunityShortsLatencyCauseReportWithOptions(
		reqCtx,
		appConfig,
		logger,
		now,
		opsapp.CommunityShortsLatencyCauseCollectOptions{PeriodSpecs: specs},
	)
	if err != nil {
		return fmt.Errorf("failed to collect community/shorts latency cause report: %w", err)
	}

	return writeLatencyCauseReport(ctx, *format, &report)
}

func writeLatencyCauseReport(ctx commandContext, format string, report *opsapp.CommunityShortsLatencyCauseReport) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "markdown":
		return writeLatencyCauseMarkdown(ctx, report)
	case "json":
		return writeLatencyCauseJSON(ctx, report)
	default:
		return fmt.Errorf("unsupported format %q (want markdown or json)", format)
	}
}

func writeLatencyCauseMarkdown(ctx commandContext, report *opsapp.CommunityShortsLatencyCauseReport) error {
	if _, err := fmt.Fprint(ctx.stdout, opsapp.RenderCommunityShortsLatencyCauseMarkdown(report)); err != nil {
		return fmt.Errorf("failed to write community/shorts latency-cause markdown: %w", err)
	}
	return nil
}

func writeLatencyCauseJSON(ctx commandContext, report *opsapp.CommunityShortsLatencyCauseReport) error {
	encoder := json.NewEncoder(ctx.stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("failed to write community/shorts latency-cause JSON: %w", err)
	}
	return nil
}

func parseLatencyPeriodSpecs(values []string) ([]opsapp.CommunityShortsLatencyPeriodSpec, error) {
	if len(values) == 0 {
		return opsapp.DefaultCommunityShortsLatencyPeriodSpecs(), nil
	}

	specs := make([]opsapp.CommunityShortsLatencyPeriodSpec, 0, len(values))
	for i := range values {
		spec, err := parseLatencyPeriodSpec(values[i])
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}

	return specs, nil
}

func parseLatencyPeriodSpec(value string) (opsapp.CommunityShortsLatencyPeriodSpec, error) {
	label, rawDuration, ok := strings.Cut(value, "=")
	if !ok {
		return opsapp.CommunityShortsLatencyPeriodSpec{}, fmt.Errorf("%q must use label=duration", value)
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return opsapp.CommunityShortsLatencyPeriodSpec{}, fmt.Errorf("%q has empty label", value)
	}
	duration, err := time.ParseDuration(strings.TrimSpace(rawDuration))
	if err != nil {
		return opsapp.CommunityShortsLatencyPeriodSpec{}, fmt.Errorf("%q has invalid duration: %w", value, err)
	}
	if duration <= 0 {
		return opsapp.CommunityShortsLatencyPeriodSpec{}, fmt.Errorf("%q must be greater than zero", value)
	}
	return opsapp.CommunityShortsLatencyPeriodSpec{Label: label, Window: duration}, nil
}
