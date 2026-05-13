package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	opsapp "github.com/kapu/hololive-stream-ingester/internal/ops/communityshorts"
)

type periodFlagValues []string

func (p *periodFlagValues) String() string {
	return strings.Join(*p, ",")
}

func (p *periodFlagValues) Set(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("period value is empty")
	}
	*p = append(*p, trimmed)
	return nil
}

func main() {
	os.Exit(runLatencyPeriodSummary())
}

func runLatencyPeriodSummary() int {
	var periods periodFlagValues
	flag.Var(&periods, "period", "period spec in label=duration form; repeatable, defaults to last_1h=1h,last_24h=24h,last_7d=168h")
	format := flag.String("format", "markdown", "output format: markdown or json")
	flag.Parse()

	specs, err := parseLatencyPeriodSpecs(periods)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid period flag: %v\n", err)
		return 1
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load community/shorts latency-period config: %v\n", err)
		return 1
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := opsapp.CollectCommunityShortsLatencyPeriodReport(ctx, cfg, logger, now, specs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to collect community/shorts latency period report: %v\n", err)
		return 1
	}

	if err := writeLatencyPeriodReport(*format, report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func writeLatencyPeriodReport(format string, report opsapp.CommunityShortsLatencyPeriodReport) error {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "markdown":
		return writeLatencyPeriodMarkdown(report)
	case "json":
		return writeLatencyPeriodJSON(report)
	default:
		return fmt.Errorf("unsupported format %q (want markdown or json)", format)
	}
}

func writeLatencyPeriodMarkdown(report opsapp.CommunityShortsLatencyPeriodReport) error {
	if _, err := fmt.Print(opsapp.RenderCommunityShortsLatencyPeriodMarkdown(report)); err != nil {
		return fmt.Errorf("failed to write community/shorts latency period markdown: %w", err)
	}
	return nil
}

func writeLatencyPeriodJSON(report opsapp.CommunityShortsLatencyPeriodReport) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("failed to write community/shorts latency period JSON: %w", err)
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
