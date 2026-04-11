package main

import (
	"context"
	"encoding/json"
	"errors"
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
	observationRuntime := flag.String("observation-runtime", "", "runtime name for a specific observation window")
	observationCutover := flag.String("observation-cutover", "", "RFC3339 cutover timestamp for a specific observation window")
	format := flag.String("format", "markdown", "output format: markdown or json")
	flag.Parse()

	if err := validateCommunityShortsLatencyCauseCLIArgs(periods, *observationRuntime, *observationCutover); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	useObservationQuery := strings.TrimSpace(*observationRuntime) != "" || strings.TrimSpace(*observationCutover) != ""
	options := app.CommunityShortsLatencyCauseCollectOptions{}
	if useObservationQuery {
		parsedCutoverAt, err := time.Parse(time.RFC3339, strings.TrimSpace(*observationCutover))
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid observation-cutover %q: %v\n", *observationCutover, err)
			os.Exit(1)
		}
		parsedCutoverAt = parsedCutoverAt.UTC()
		options.ObservationRuntimeName = strings.TrimSpace(*observationRuntime)
		options.ObservationBigBangCutoverAt = &parsedCutoverAt
	} else {
		specs, err := parseLatencyCausePeriodSpecs(periods)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid period flag: %v\n", err)
			os.Exit(1)
		}
		options.PeriodSpecs = specs
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load community/shorts latency-cause config: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := app.CollectCommunityShortsLatencyCauseReportWithOptions(ctx, cfg, logger, now, options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to collect community/shorts latency cause report: %v\n", err)
		os.Exit(1)
	}

	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "markdown":
		if _, err := fmt.Print(app.RenderCommunityShortsLatencyCauseMarkdown(report)); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write community/shorts latency-cause markdown: %v\n", err)
			os.Exit(1)
		}
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write community/shorts latency-cause JSON: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unsupported format %q (want markdown or json)\n", *format)
		os.Exit(1)
	}
}

func parseLatencyCausePeriodSpecs(values []string) ([]app.CommunityShortsLatencyPeriodSpec, error) {
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

func validateCommunityShortsLatencyCauseCLIArgs(
	periods []string,
	observationRuntime string,
	observationCutover string,
) error {
	trimmedRuntime := strings.TrimSpace(observationRuntime)
	trimmedCutover := strings.TrimSpace(observationCutover)
	useObservationQuery := trimmedRuntime != "" || trimmedCutover != ""
	if !useObservationQuery {
		return nil
	}
	if len(periods) > 0 {
		return errors.New("period and observation query flags are mutually exclusive")
	}
	if trimmedRuntime == "" || trimmedCutover == "" {
		return errors.New("observation-runtime and observation-cutover must be provided together")
	}
	return nil
}
