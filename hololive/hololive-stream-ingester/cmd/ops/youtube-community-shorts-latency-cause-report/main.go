package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kapu/hololive-stream-ingester/cmd/ops/internal/observationquery"
	"github.com/kapu/hololive-stream-ingester/cmd/ops/internal/reportcli"
	opsapp "github.com/kapu/hololive-stream-ingester/internal/ops"
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

	err := reportcli.RunOptionalObservationReport(
		reportcli.OptionalObservationParams{
			Runtime: *observationRuntime,
			Cutover: *observationCutover,
			Format:  *format,
		},
		reportcli.OptionalObservationCommand[
			opsapp.CommunityShortsLatencyCauseCollectOptions,
			opsapp.CommunityShortsLatencyCauseReport,
		]{
			BuildOptions: func(_ time.Time, query reportcli.ObservationQuery, useObservationQuery bool) (opsapp.CommunityShortsLatencyCauseCollectOptions, error) {
				if err := validateCommunityShortsLatencyCauseCLIArgs(periods, *observationRuntime, *observationCutover); err != nil {
					return opsapp.CommunityShortsLatencyCauseCollectOptions{}, err
				}
				options := opsapp.CommunityShortsLatencyCauseCollectOptions{}
				if useObservationQuery {
					options.ObservationRuntimeName = query.Runtime
					options.ObservationBigBangCutoverAt = &query.CutoverAt
					return options, nil
				}
				specs, err := parseLatencyCausePeriodSpecs(periods)
				if err != nil {
					return opsapp.CommunityShortsLatencyCauseCollectOptions{}, fmt.Errorf("invalid period flag: %w", err)
				}
				options.PeriodSpecs = specs
				return options, nil
			},
			Collect:            opsapp.CollectCommunityShortsLatencyCauseReportWithOptions,
			RenderMarkdown:     opsapp.RenderCommunityShortsLatencyCauseMarkdown,
			LoadConfigError:    "Failed to load community/shorts latency-cause config",
			CollectError:       "Failed to collect community/shorts latency cause report",
			MarkdownWriteError: "Failed to write community/shorts latency-cause markdown",
			JSONWriteError:     "Failed to write community/shorts latency-cause JSON",
		},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func parseLatencyCausePeriodSpecs(values []string) ([]opsapp.CommunityShortsLatencyPeriodSpec, error) {
	if len(values) == 0 {
		return opsapp.DefaultCommunityShortsLatencyPeriodSpecs(), nil
	}

	specs := make([]opsapp.CommunityShortsLatencyPeriodSpec, 0, len(values))
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
		specs = append(specs, opsapp.CommunityShortsLatencyPeriodSpec{Label: label, Window: duration})
	}

	return specs, nil
}

func validateCommunityShortsLatencyCauseCLIArgs(
	periods []string,
	observationRuntime string,
	observationCutover string,
) error {
	_, useObservationQuery, err := observationquery.ParseOptional(observationRuntime, observationCutover)
	if err != nil {
		return err
	}
	if !useObservationQuery {
		return nil
	}
	if len(periods) > 0 {
		return errors.New("period and observation query flags are mutually exclusive")
	}
	return nil
}
