package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-stream-ingester/internal/app"
)

const continuousObservationRetryInterval = 5 * time.Second

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
	options := app.CommunityShortsContinuousObservationCollectOptions{
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

		if report.Observation.Status == app.CommunityShortsContinuousObservationStatusFinalized {
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

type continuousObservationOutputPaths struct {
	latest   string
	snapshot string
}

func collectContinuousObservationWithWait(
	cfg *config.Config,
	logger *slog.Logger,
	options app.CommunityShortsContinuousObservationCollectOptions,
	waitTimeout time.Duration,
) (app.CommunityShortsContinuousObservationReport, error) {
	deadline := time.Now().Add(waitTimeout)
	for {
		report, err := collectContinuousObservationOnce(cfg, logger, options)
		if err == nil {
			return report, nil
		}
		if time.Now().After(deadline) || !isObservationWindowNotFoundError(err) {
			return app.CommunityShortsContinuousObservationReport{}, err
		}
		time.Sleep(continuousObservationRetryInterval)
	}
}

func collectContinuousObservationOnce(
	cfg *config.Config,
	logger *slog.Logger,
	options app.CommunityShortsContinuousObservationCollectOptions,
) (app.CommunityShortsContinuousObservationReport, error) {
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return app.CollectCommunityShortsContinuousObservationReport(ctx, cfg, logger, now, options)
}

func renderContinuousObservationOutput(
	report app.CommunityShortsContinuousObservationReport,
	format string,
) ([]byte, string, error) {
	switch format {
	case "markdown":
		return []byte(app.RenderCommunityShortsContinuousObservationMarkdown(report)), ".md", nil
	case "json":
		payload, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return nil, "", err
		}
		payload = append(payload, '\n')
		return payload, ".json", nil
	default:
		return nil, "", fmt.Errorf("unsupported format %q", format)
	}
}

func writeContinuousObservationSnapshot(
	dir string,
	ext string,
	report app.CommunityShortsContinuousObservationReport,
	payload []byte,
) (continuousObservationOutputPaths, error) {
	timestamp := report.GeneratedAt.UTC().Format("20060102-150405")
	snapshotPath := filepath.Join(dir, fmt.Sprintf("snapshot-%s%s", timestamp, ext))
	latestPath := filepath.Join(dir, fmt.Sprintf("latest%s", ext))
	if err := os.WriteFile(snapshotPath, payload, 0o644); err != nil {
		return continuousObservationOutputPaths{}, err
	}
	if err := os.WriteFile(latestPath, payload, 0o644); err != nil {
		return continuousObservationOutputPaths{}, err
	}
	return continuousObservationOutputPaths{latest: latestPath, snapshot: snapshotPath}, nil
}

func nextContinuousObservationInterval(report app.CommunityShortsContinuousObservationReport) time.Duration {
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

func defaultContinuousObservationOutputDir(runtimeName string, cutoverAt time.Time) string {
	sanitizedRuntimeName := strings.TrimSpace(runtimeName)
	if sanitizedRuntimeName == "" {
		sanitizedRuntimeName = "youtube-scraper"
	}
	cutoverLabel := cutoverAt.UTC().Format("20060102T150405Z")
	return filepath.Join("artifacts", "youtube-community-shorts-continuous-observation", sanitizedRuntimeName+"-"+cutoverLabel)
}

func parseContinuousObservationCutover(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, errors.New("observation-cutover is required")
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid observation-cutover %q: %v", raw, err)
	}
	return parsed.UTC(), nil
}

func isObservationWindowNotFoundError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "observation window not found")
}
