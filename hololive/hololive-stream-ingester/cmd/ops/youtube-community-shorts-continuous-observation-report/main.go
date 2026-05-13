package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	opsapp "github.com/kapu/hololive-stream-ingester/internal/ops/communityshorts"
)

type continuousObservationCLIOptions struct {
	observationRuntime string
	parsedCutoverAt    time.Time
	format             string
	outputDir          string
	watch              bool
	deliveryLogLimit   int
	waitTimeout        time.Duration
}

func main() {
	options, err := parseContinuousObservationCLIOptions()
	if err != nil {
		exitContinuousObservation("%s", err.Error())
	}

	cfg, err := config.Load()
	if err != nil {
		exitContinuousObservation("Failed to load community/shorts continuous observation config: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := runContinuousObservationReport(cfg, logger, options); err != nil {
		exitContinuousObservation("%s", err.Error())
	}
}

func parseContinuousObservationCLIOptions() (continuousObservationCLIOptions, error) {
	observationRuntime := flag.String("observation-runtime", "youtube-scraper", "runtime name for a specific observation window")
	observationCutover := flag.String("observation-cutover", "", "RFC3339 cutover timestamp for a specific observation window")
	format := flag.String("format", "markdown", "output format: markdown or json")
	outputDir := flag.String("output-dir", "", "directory to write latest and snapshot files; defaults to artifacts/... in watch mode")
	watch := flag.Bool("watch", false, "keep collecting snapshots until the observation window is finalized")
	deliveryLogLimit := flag.Int("delivery-log-limit", 200, "maximum delivery log rows per snapshot")
	waitTimeout := flag.Duration("wait-timeout", 2*time.Minute, "how long to wait for the observation window to appear before failing")
	flag.Parse()

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

func runContinuousObservationReport(cfg *config.Config, logger *slog.Logger, cliOptions continuousObservationCLIOptions) error {
	options := opsapp.CommunityShortsContinuousObservationCollectOptions{
		ObservationRuntimeName:      cliOptions.observationRuntime,
		ObservationBigBangCutoverAt: cliOptions.parsedCutoverAt,
		DeliveryLogLimit:            cliOptions.deliveryLogLimit,
	}

	if cliOptions.watch {
		return runContinuousObservationWatch(cfg, logger, options, cliOptions)
	}
	return runContinuousObservationOnce(cfg, logger, options, cliOptions)
}

func runContinuousObservationOnce(
	cfg *config.Config,
	logger *slog.Logger,
	options opsapp.CommunityShortsContinuousObservationCollectOptions,
	cliOptions continuousObservationCLIOptions,
) error {
	report, err := collectContinuousObservationWithWait(cfg, logger, options, cliOptions.waitTimeout)
	if err != nil {
		return fmt.Errorf("failed to collect continuous observation report: %w", err)
	}
	return writeContinuousObservationReport(cliOptions.outputDir, cliOptions.format, report)
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

func writeContinuousObservationReport(outputDir string, format string, report opsapp.CommunityShortsContinuousObservationReport) error {
	payload, ext, err := renderContinuousObservationOutput(report, format)
	if err != nil {
		return fmt.Errorf("failed to render continuous observation report: %w", err)
	}
	if outputDir == "" {
		return writeContinuousObservationStdout(payload)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	if _, err := writeContinuousObservationSnapshot(outputDir, ext, report, payload); err != nil {
		return fmt.Errorf("failed to write continuous observation report files: %w", err)
	}
	return nil
}

func writeContinuousObservationStdout(payload []byte) error {
	if _, err := os.Stdout.Write(payload); err != nil {
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

func exitContinuousObservation(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
