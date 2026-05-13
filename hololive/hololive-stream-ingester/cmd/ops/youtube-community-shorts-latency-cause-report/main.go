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
	flags := parseLatencyCauseFlags()

	err := reportcli.RunOptionalObservationReport(
		latencyCauseReportParams(flags),
		latencyCauseReportCommand(flags),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

type latencyCauseFlags struct {
	periods            periodFlagValues
	observationRuntime *string
	observationCutover *string
	format             *string
}

func parseLatencyCauseFlags() latencyCauseFlags {
	var periods periodFlagValues
	flag.Var(&periods, "period", "period spec in label=duration form; repeatable, defaults to last_1h=1h,last_24h=24h,last_7d=168h")
	flags := latencyCauseFlags{
		periods:            periods,
		observationRuntime: flag.String("observation-runtime", "", "runtime name for a specific observation window"),
		observationCutover: flag.String("observation-cutover", "", "RFC3339 cutover timestamp for a specific observation window"),
		format:             flag.String("format", "markdown", "output format: markdown or json"),
	}
	flag.Parse()
	flags.periods = periods
	return flags
}

func latencyCauseReportParams(flags latencyCauseFlags) reportcli.OptionalObservationParams {
	return reportcli.OptionalObservationParams{
		Runtime: *flags.observationRuntime,
		Cutover: *flags.observationCutover,
		Format:  *flags.format,
	}
}

func latencyCauseReportCommand(flags latencyCauseFlags) reportcli.OptionalObservationCommand[
	opsapp.CommunityShortsLatencyCauseCollectOptions,
	opsapp.CommunityShortsLatencyCauseReport,
] {
	return reportcli.OptionalObservationCommand[
		opsapp.CommunityShortsLatencyCauseCollectOptions,
		opsapp.CommunityShortsLatencyCauseReport,
	]{
		BuildOptions:       buildLatencyCauseOptions(flags),
		Collect:            opsapp.CollectCommunityShortsLatencyCauseReportWithOptions,
		RenderMarkdown:     opsapp.RenderCommunityShortsLatencyCauseMarkdown,
		LoadConfigError:    "Failed to load community/shorts latency-cause config",
		CollectError:       "Failed to collect community/shorts latency cause report",
		JSONWriteError:     "Failed to write community/shorts latency-cause JSON",
		MarkdownWriteError: "Failed to write community/shorts latency-cause markdown",
	}
}

func buildLatencyCauseOptions(flags latencyCauseFlags) func(
	time.Time,
	reportcli.ObservationQuery,
	bool,
) (opsapp.CommunityShortsLatencyCauseCollectOptions, error) {
	return func(_ time.Time, query reportcli.ObservationQuery, useObservationQuery bool) (opsapp.CommunityShortsLatencyCauseCollectOptions, error) {
		return newLatencyCauseCollectOptions(flags, query, useObservationQuery)
	}
}

func newLatencyCauseCollectOptions(
	flags latencyCauseFlags,
	query reportcli.ObservationQuery,
	useObservationQuery bool,
) (opsapp.CommunityShortsLatencyCauseCollectOptions, error) {
	if err := validateCommunityShortsLatencyCauseCLIArgs(flags.periods, *flags.observationRuntime, *flags.observationCutover); err != nil {
		return opsapp.CommunityShortsLatencyCauseCollectOptions{}, err
	}
	options := opsapp.CommunityShortsLatencyCauseCollectOptions{}
	if useObservationQuery {
		options.ObservationRuntimeName = query.Runtime
		options.ObservationBigBangCutoverAt = &query.CutoverAt
		return options, nil
	}
	specs, err := parseLatencyCausePeriodSpecs(flags.periods)
	if err != nil {
		return opsapp.CommunityShortsLatencyCauseCollectOptions{}, fmt.Errorf("invalid period flag: %w", err)
	}
	options.PeriodSpecs = specs
	return options, nil
}

func parseLatencyCausePeriodSpecs(values []string) ([]opsapp.CommunityShortsLatencyPeriodSpec, error) {
	if len(values) == 0 {
		return opsapp.DefaultCommunityShortsLatencyPeriodSpecs(), nil
	}

	specs := make([]opsapp.CommunityShortsLatencyPeriodSpec, 0, len(values))
	for i := range values {
		spec, err := parseLatencyCausePeriodSpec(values[i])
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}

	return specs, nil
}

func parseLatencyCausePeriodSpec(value string) (opsapp.CommunityShortsLatencyPeriodSpec, error) {
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
