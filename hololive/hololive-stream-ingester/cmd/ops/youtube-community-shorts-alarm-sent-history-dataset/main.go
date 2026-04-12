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
	opsapp "github.com/kapu/hololive-stream-ingester/internal/ops"
)

func main() {
	observationRuntime := flag.String("observation-runtime", "", "runtime name for a specific observation window")
	observationCutover := flag.String("observation-cutover", "", "RFC3339 cutover timestamp for a specific observation window")
	format := flag.String("format", "markdown", "output format: markdown or json")
	flag.Parse()

	if err := validateCommunityShortsAlarmSentHistoryDatasetCLIArgs(*observationRuntime, *observationCutover); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	parsedCutoverAt, err := time.Parse(time.RFC3339, strings.TrimSpace(*observationCutover))
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid observation-cutover %q: %v\n", *observationCutover, err)
		os.Exit(1)
	}
	parsedCutoverAt = parsedCutoverAt.UTC()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load community/shorts alarm sent-history dataset config: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := opsapp.CollectCommunityShortsAlarmSentHistoryDatasetReport(ctx, cfg, logger, now, opsapp.CommunityShortsAlarmSentHistoryDatasetCollectOptions{
		ObservationRuntimeName:      strings.TrimSpace(*observationRuntime),
		ObservationBigBangCutoverAt: &parsedCutoverAt,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to collect community/shorts alarm sent history dataset: %v\n", err)
		os.Exit(1)
	}

	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "markdown":
		if _, err := fmt.Print(opsapp.RenderCommunityShortsAlarmSentHistoryDatasetMarkdown(report)); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write community/shorts alarm sent-history dataset markdown: %v\n", err)
			os.Exit(1)
		}
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write community/shorts alarm sent-history dataset JSON: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unsupported format %q (want markdown or json)\n", *format)
		os.Exit(1)
	}
}

func validateCommunityShortsAlarmSentHistoryDatasetCLIArgs(observationRuntime string, observationCutover string) error {
	trimmedRuntime := strings.TrimSpace(observationRuntime)
	trimmedCutover := strings.TrimSpace(observationCutover)
	if trimmedRuntime == "" && trimmedCutover == "" {
		return errors.New("observation-runtime and observation-cutover are required")
	}
	if trimmedRuntime == "" || trimmedCutover == "" {
		return errors.New("observation-runtime and observation-cutover must be provided together")
	}
	return nil
}
