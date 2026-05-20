package communityshortscli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	opsapp "github.com/kapu/hololive-youtube-producer/internal/ops/communityshorts"
)

const continuousObservationRetryInterval = 5 * time.Second

type continuousObservationCLIOptions struct {
	observationRuntime string
	parsedCutoverAt    time.Time
	format             string
	outputDir          string
	watch              bool
	deliveryLogLimit   int
	waitTimeout        time.Duration
}

type continuousObservationOutputPaths struct {
	latest   string
	snapshot string
}

func runContinuousObservationCommand(ctx commandContext, args []string) error {
	options, err := parseContinuousObservationCLIOptions(ctx, args)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load community/shorts continuous observation config: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(ctx.stderr, nil))
	return runContinuousObservationReport(ctx, cfg, logger, options)
}

func parseContinuousObservationCLIOptions(ctx commandContext, args []string) (continuousObservationCLIOptions, error) {
	fs := newFlagSet(ctx, "continuous-observation-report")
	observationRuntime := fs.String("observation-runtime", "youtube-producer", "runtime name for a specific observation window")
	observationCutover := fs.String("observation-cutover", "", "RFC3339 cutover timestamp for a specific observation window")
	format := fs.String("format", "markdown", "output format: markdown or json")
	outputDir := fs.String("output-dir", "", "directory to write latest and snapshot files; defaults to artifacts/... in watch mode")
	watch := fs.Bool("watch", false, "keep collecting snapshots until the observation window is finalized")
	deliveryLogLimit := fs.Int("delivery-log-limit", 200, "maximum delivery log rows per snapshot")
	waitTimeout := fs.Duration("wait-timeout", 2*time.Minute, "how long to wait for the observation window to appear before failing")
	if err := fs.Parse(args); err != nil {
		return continuousObservationCLIOptions{}, err
	}

	parsedCutoverAt, err := parseContinuousObservationCutover(*observationCutover)
	if err != nil {
		return continuousObservationCLIOptions{}, err
	}

	options := continuousObservationCLIOptions{
		observationRuntime: strings.TrimSpace(*observationRuntime),
		parsedCutoverAt:    parsedCutoverAt,
		format:             strings.TrimSpace(*format),
		outputDir:          strings.TrimSpace(*outputDir),
		watch:              *watch,
		deliveryLogLimit:   *deliveryLogLimit,
		waitTimeout:        *waitTimeout,
	}
	return options, validateContinuousObservationCLIOptions(options)
}

func validateContinuousObservationCLIOptions(options continuousObservationCLIOptions) error {
	if options.format != "markdown" && options.format != "json" {
		return fmt.Errorf("unsupported format %q (want markdown or json)", options.format)
	}
	if options.deliveryLogLimit <= 0 {
		return fmt.Errorf("delivery-log-limit must be greater than zero")
	}
	if options.waitTimeout <= 0 {
		return fmt.Errorf("wait-timeout must be greater than zero")
	}
	return nil
}

func runContinuousObservationReport(
	ctx commandContext,
	cfg *config.Config,
	logger *slog.Logger,
	cliOptions continuousObservationCLIOptions,
) error {
	options := opsapp.CommunityShortsContinuousObservationCollectOptions{
		ObservationRuntimeName:      cliOptions.observationRuntime,
		ObservationBigBangCutoverAt: cliOptions.parsedCutoverAt,
		DeliveryLogLimit:            cliOptions.deliveryLogLimit,
	}

	if cliOptions.watch {
		return runContinuousObservationWatch(cfg, logger, options, cliOptions)
	}
	return runContinuousObservationOnce(ctx, cfg, logger, options, cliOptions)
}

func runContinuousObservationOnce(
	ctx commandContext,
	cfg *config.Config,
	logger *slog.Logger,
	options opsapp.CommunityShortsContinuousObservationCollectOptions,
	cliOptions continuousObservationCLIOptions,
) error {
	report, err := collectContinuousObservationWithWait(cfg, logger, options, cliOptions.waitTimeout)
	if err != nil {
		return fmt.Errorf("failed to collect continuous observation report: %w", err)
	}
	return writeContinuousObservationReport(ctx, cliOptions.outputDir, cliOptions.format, report)
}

func runContinuousObservationWatch(
	cfg *config.Config,
	logger *slog.Logger,
	options opsapp.CommunityShortsContinuousObservationCollectOptions,
	cliOptions continuousObservationCLIOptions,
) error {
	dir, err := prepareContinuousObservationWatchDir(cliOptions.outputDir, options)
	if err != nil {
		return err
	}

	report, err := collectContinuousObservationWithWait(cfg, logger, options, cliOptions.waitTimeout)
	if err != nil {
		return fmt.Errorf("failed to collect continuous observation report: %w", err)
	}

	return runContinuousObservationWatchLoop(cfg, logger, options, cliOptions, dir, report)
}

func runContinuousObservationWatchLoop(
	cfg *config.Config,
	logger *slog.Logger,
	options opsapp.CommunityShortsContinuousObservationCollectOptions,
	cliOptions continuousObservationCLIOptions,
	dir string,
	report opsapp.CommunityShortsContinuousObservationReport,
) error {
	for {
		if err := writeContinuousObservationWatchSnapshot(logger, dir, cliOptions.format, report); err != nil {
			return err
		}
		if report.Observation.Status == opsapp.CommunityShortsContinuousObservationStatusFinalized {
			break
		}

		time.Sleep(nextContinuousObservationInterval(report))
		refreshedReport, err := collectContinuousObservationOnce(cfg, logger, options)
		if err != nil {
			return fmt.Errorf("failed to refresh continuous observation report: %w", err)
		}
		report = refreshedReport
	}
	return nil
}

func prepareContinuousObservationWatchDir(dir string, options opsapp.CommunityShortsContinuousObservationCollectOptions) (string, error) {
	if dir == "" {
		dir = defaultContinuousObservationOutputDir(options.ObservationRuntimeName, options.ObservationBigBangCutoverAt)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}
	return dir, nil
}

func writeContinuousObservationReport(ctx commandContext, outputDir string, format string, report opsapp.CommunityShortsContinuousObservationReport) error {
	payload, ext, err := renderContinuousObservationOutput(report, format)
	if err != nil {
		return fmt.Errorf("failed to render continuous observation report: %w", err)
	}
	if outputDir == "" {
		return writeContinuousObservationStdout(ctx, payload)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	if _, err := writeContinuousObservationSnapshot(outputDir, ext, report, payload); err != nil {
		return fmt.Errorf("failed to write continuous observation report files: %w", err)
	}
	return nil
}

func writeContinuousObservationStdout(ctx commandContext, payload []byte) error {
	if _, err := ctx.stdout.Write(payload); err != nil {
		return fmt.Errorf("failed to write continuous observation report: %w", err)
	}
	return nil
}

func writeContinuousObservationWatchSnapshot(logger *slog.Logger, dir string, format string, report opsapp.CommunityShortsContinuousObservationReport) error {
	payload, ext, err := renderContinuousObservationOutput(report, format)
	if err != nil {
		return fmt.Errorf("failed to render continuous observation report: %w", err)
	}
	paths, err := writeContinuousObservationSnapshot(dir, ext, report, payload)
	if err != nil {
		return fmt.Errorf("failed to write continuous observation report files: %w", err)
	}
	logger.Info("YouTube community/shorts continuous observation snapshot written",
		slog.String("latest_path", paths.latest),
		slog.String("snapshot_path", paths.snapshot),
		slog.String("observation_status", string(report.Observation.Status)),
		slog.Time("observed_until", report.Observation.ObservedUntil),
	)
	return nil
}

func collectContinuousObservationWithWait(
	cfg *config.Config,
	logger *slog.Logger,
	options opsapp.CommunityShortsContinuousObservationCollectOptions,
	waitTimeout time.Duration,
) (opsapp.CommunityShortsContinuousObservationReport, error) {
	deadline := time.Now().Add(waitTimeout)
	for {
		report, err := collectContinuousObservationOnce(cfg, logger, options)
		if err == nil {
			return report, nil
		}
		if time.Now().After(deadline) || !isObservationWindowNotFoundError(err) {
			return opsapp.CommunityShortsContinuousObservationReport{}, err
		}
		time.Sleep(continuousObservationRetryInterval)
	}
}

func collectContinuousObservationOnce(
	cfg *config.Config,
	logger *slog.Logger,
	options opsapp.CommunityShortsContinuousObservationCollectOptions,
) (opsapp.CommunityShortsContinuousObservationReport, error) {
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return opsapp.CollectCommunityShortsContinuousObservationReport(ctx, cfg, logger, now, options)
}

func nextContinuousObservationInterval(report opsapp.CommunityShortsContinuousObservationReport) time.Duration {
	interval := 15 * time.Minute
	if report.Observation.ObservedUntil.Sub(report.Observation.ObservationStartedAt) < time.Hour {
		interval = 5 * time.Minute
	}
	remaining := report.Observation.ObservationEndsAt.Sub(report.GeneratedAt.UTC())
	if remaining > 0 && remaining < interval {
		return remaining
	}
	if remaining <= 0 {
		return continuousObservationRetryInterval
	}
	return interval
}

func isObservationWindowNotFoundError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "observation window not found")
}
