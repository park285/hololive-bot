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
	"github.com/kapu/hololive-stream-ingester/internal/app"
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
	var periods periodFlagValues
	flag.Var(&periods, "period", "period spec in label=duration form; repeatable, defaults to last_1h=1h,last_24h=24h,last_7d=168h")
	format := flag.String("format", "markdown", "output format: markdown or json")
	flag.Parse()

	specs, err := parseLatencyPeriodSpecs(periods)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid period flag: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load community/shorts latency-period config: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := app.CollectCommunityShortsLatencyPeriodReport(ctx, cfg, logger, now, specs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to collect community/shorts latency period report: %v\n", err)
		os.Exit(1)
	}

	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "markdown":
		if _, err := fmt.Print(app.RenderCommunityShortsLatencyPeriodMarkdown(report)); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write community/shorts latency period markdown: %v\n", err)
			os.Exit(1)
		}
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write community/shorts latency period JSON: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unsupported format %q (want markdown or json)\n", *format)
		os.Exit(1)
	}
}

func parseLatencyPeriodSpecs(values []string) ([]app.CommunityShortsLatencyPeriodSpec, error) {
	if len(values) == 0 {
		return app.DefaultCommunityShortsLatencyPeriodSpecs(), nil
	}

	specs := make([]app.CommunityShortsLatencyPeriodSpec, 0, len(values))
	for i := range values {
		label, rawDuration, ok := strings.Cut(values[i], "=")
		if !ok {
			return nil, fmt.Errorf("%q must use label=duration", values[i])
		}
		label = strings.TrimSpace(label)
		if label == "" {
			return nil, fmt.Errorf("%q has empty label", values[i])
		}
		duration, err := time.ParseDuration(strings.TrimSpace(rawDuration))
		if err != nil {
			return nil, fmt.Errorf("%q has invalid duration: %w", values[i], err)
		}
		if duration <= 0 {
			return nil, fmt.Errorf("%q must be greater than zero", values[i])
		}
		specs = append(specs, app.CommunityShortsLatencyPeriodSpec{Label: label, Window: duration})
	}

	return specs, nil
}
