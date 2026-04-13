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
	"github.com/kapu/hololive-stream-ingester/cmd/ops/internal/observationquery"
	opsapp "github.com/kapu/hololive-stream-ingester/internal/ops"
)

func main() {
	window := flag.Duration("window", 24*time.Hour, "lookback window for recent community/shorts per-post send counts")
	observationRuntime := flag.String("observation-runtime", "", "runtime name for a specific observation window")
	observationCutover := flag.String("observation-cutover", "", "RFC3339 cutover timestamp for a specific observation window")
	format := flag.String("format", "markdown", "output format: markdown or json")
	flag.Parse()

	windowExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "window" {
			windowExplicit = true
		}
	})

	if err := validateCommunityShortsSendCountCLIArgs(*window, windowExplicit, *observationRuntime, *observationCutover); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	observationQuery, useObservationQuery, err := observationquery.ParseOptional(*observationRuntime, *observationCutover)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	var observationCutoverAt *time.Time
	if useObservationQuery {
		observationCutoverAt = &observationQuery.CutoverAt
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load community/shorts send-count config: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	options := opsapp.CommunityShortsSendCountCollectOptions{
		ObservationRuntimeName:      observationQuery.Runtime,
		ObservationBigBangCutoverAt: observationCutoverAt,
	}
	if !useObservationQuery {
		since := now.Add(-*window)
		options.Since = &since
	}

	report, err := opsapp.CollectCommunityShortsSendCountReportWithOptions(ctx, cfg, logger, now, options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to collect community/shorts send counts: %v\n", err)
		os.Exit(1)
	}

	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "markdown":
		if _, err := fmt.Print(opsapp.RenderCommunityShortsSendCountMarkdown(report)); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write community/shorts send-count markdown: %v\n", err)
			os.Exit(1)
		}
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write community/shorts send-count JSON: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unsupported format %q (want markdown or json)\n", *format)
		os.Exit(1)
	}
}

func validateCommunityShortsSendCountCLIArgs(
	window time.Duration,
	windowExplicit bool,
	observationRuntime string,
	observationCutover string,
) error {
	_, useObservationQuery, err := observationquery.ParseOptional(observationRuntime, observationCutover)
	if err != nil {
		return err
	}

	if !useObservationQuery {
		if window <= 0 {
			return errors.New("window must be greater than zero")
		}
		return nil
	}
	if windowExplicit {
		return errors.New("window and observation query flags are mutually exclusive")
	}
	return nil
}
