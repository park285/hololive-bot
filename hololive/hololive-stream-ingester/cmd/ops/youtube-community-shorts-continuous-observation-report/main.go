package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	opsapp "github.com/kapu/hololive-stream-ingester/internal/ops"
)

func main() {
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
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	if strings.TrimSpace(*format) != "markdown" && strings.TrimSpace(*format) != "json" {
		fmt.Fprintf(os.Stderr, "unsupported format %q (want markdown or json)\n", *format)
		os.Exit(1)
	}
	if *deliveryLogLimit <= 0 {
		fmt.Fprintln(os.Stderr, "delivery-log-limit must be greater than zero")
		os.Exit(1)
	}
	if *waitTimeout <= 0 {
		fmt.Fprintln(os.Stderr, "wait-timeout must be greater than zero")
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load community/shorts continuous observation config: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	options := opsapp.CommunityShortsContinuousObservationCollectOptions{
		ObservationRuntimeName:      strings.TrimSpace(*observationRuntime),
		ObservationBigBangCutoverAt: parsedCutoverAt,
		DeliveryLogLimit:            *deliveryLogLimit,
	}

	if !*watch {
		report, err := collectContinuousObservationWithWait(cfg, logger, options, *waitTimeout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to collect continuous observation report: %v\n", err)
			os.Exit(1)
		}
		payload, ext, err := renderContinuousObservationOutput(report, strings.TrimSpace(*format))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to render continuous observation report: %v\n", err)
			os.Exit(1)
		}
		if strings.TrimSpace(*outputDir) == "" {
			if _, err := os.Stdout.Write(payload); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to write continuous observation report: %v\n", err)
				os.Exit(1)
			}
			return
		}
		if err := os.MkdirAll(strings.TrimSpace(*outputDir), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create output directory: %v\n", err)
			os.Exit(1)
		}
		if _, err := writeContinuousObservationSnapshot(strings.TrimSpace(*outputDir), ext, report, payload); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write continuous observation report files: %v\n", err)
			os.Exit(1)
		}
		return
	}

	dir := strings.TrimSpace(*outputDir)
	if dir == "" {
		dir = defaultContinuousObservationOutputDir(options.ObservationRuntimeName, options.ObservationBigBangCutoverAt)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create output directory: %v\n", err)
		os.Exit(1)
	}

	report, err := collectContinuousObservationWithWait(cfg, logger, options, *waitTimeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to collect continuous observation report: %v\n", err)
		os.Exit(1)
	}

	for {
		payload, ext, renderErr := renderContinuousObservationOutput(report, strings.TrimSpace(*format))
		if renderErr != nil {
			fmt.Fprintf(os.Stderr, "Failed to render continuous observation report: %v\n", renderErr)
			os.Exit(1)
		}
		paths, writeErr := writeContinuousObservationSnapshot(dir, ext, report, payload)
		if writeErr != nil {
			fmt.Fprintf(os.Stderr, "Failed to write continuous observation report files: %v\n", writeErr)
			os.Exit(1)
		}
		logger.Info("YouTube community/shorts continuous observation snapshot written",
			slog.String("latest_path", paths.latest),
			slog.String("snapshot_path", paths.snapshot),
			slog.String("observation_status", string(report.Observation.Status)),
			slog.Time("observed_until", report.Observation.ObservedUntil),
		)

		if report.Observation.Status == opsapp.CommunityShortsContinuousObservationStatusFinalized {
			break
		}

		time.Sleep(nextContinuousObservationInterval(report))
		report, err = collectContinuousObservationOnce(cfg, logger, options)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to refresh continuous observation report: %v\n", err)
			os.Exit(1)
		}
	}
}
