package communityshortscli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-stream-ingester/cmd/ops/internal/observationquery"
	"github.com/kapu/hololive-stream-ingester/cmd/ops/internal/reportcli"
	opsapp "github.com/kapu/hololive-stream-ingester/internal/ops/communityshorts"
)

type latencyCauseFlags struct {
	periods            periodFlagValues
	observationRuntime *string
	observationCutover *string
	format             *string
}

func runLatencyCauseCommand(ctx commandContext, args []string) error {
	flags, err := parseLatencyCauseFlags(ctx, args)
	if err != nil {
		return err
	}

	return reportcli.RunOptionalObservationReport(
		latencyCauseReportParams(flags),
		latencyCauseReportCommand(ctx, flags),
	)
}

func parseLatencyCauseFlags(ctx commandContext, args []string) (latencyCauseFlags, error) {
	fs := newFlagSet(ctx, "latency-cause-report")
	var periods periodFlagValues
	fs.Var(&periods, "period", "period spec in label=duration form; repeatable, defaults to last_1h=1h,last_24h=24h,last_7d=168h")
	flags := latencyCauseFlags{
		periods:            periods,
		observationRuntime: fs.String("observation-runtime", "", "runtime name for a specific observation window"),
		observationCutover: fs.String("observation-cutover", "", "RFC3339 cutover timestamp for a specific observation window"),
		format:             fs.String("format", "markdown", "output format: markdown or json"),
	}
	if err := fs.Parse(args); err != nil {
		return latencyCauseFlags{}, err
	}
	flags.periods = periods
	return flags, nil
}

func latencyCauseReportParams(flags latencyCauseFlags) reportcli.OptionalObservationParams {
	return reportcli.OptionalObservationParams{
		Runtime: *flags.observationRuntime,
		Cutover: *flags.observationCutover,
		Format:  *flags.format,
	}
}

func latencyCauseReportCommand(ctx commandContext, flags latencyCauseFlags) reportcli.OptionalObservationCommand[
	opsapp.CommunityShortsLatencyCauseCollectOptions,
	opsapp.CommunityShortsLatencyCauseReport,
] {
	return reportcli.OptionalObservationCommand[
		opsapp.CommunityShortsLatencyCauseCollectOptions,
		opsapp.CommunityShortsLatencyCauseReport,
	]{
		Stdout:             ctx.stdout,
		Stderr:             ctx.stderr,
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
		spec, err := parseLatencyPeriodSpec(values[i])
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
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
