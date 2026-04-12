package main

import (
	"context"
	"encoding/json"
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
	window := flag.Duration("window", 24*time.Hour, "lookback window for recent community/shorts delivery logs")
	observationRuntime := flag.String("observation-runtime", "", "runtime name for a specific observation window")
	observationCutover := flag.String("observation-cutover", "", "RFC3339 cutover timestamp for a specific observation window")
	limit := flag.Int("limit", 200, "maximum number of delivery log rows to return")
	format := flag.String("format", "markdown", "output format: markdown or json")
	flag.Parse()

	useObservationQuery := strings.TrimSpace(*observationRuntime) != "" || strings.TrimSpace(*observationCutover) != ""
	if !useObservationQuery && *window <= 0 {
		fmt.Fprintln(os.Stderr, "window must be greater than zero")
		os.Exit(1)
	}
	if *limit <= 0 {
		fmt.Fprintln(os.Stderr, "limit must be greater than zero")
		os.Exit(1)
	}

	var observationCutoverAt *time.Time
	if useObservationQuery {
		if strings.TrimSpace(*observationRuntime) == "" || strings.TrimSpace(*observationCutover) == "" {
			fmt.Fprintln(os.Stderr, "observation-runtime and observation-cutover must be provided together")
			os.Exit(1)
		}
		parsedCutoverAt, err := time.Parse(time.RFC3339, strings.TrimSpace(*observationCutover))
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid observation-cutover %q: %v\n", *observationCutover, err)
			os.Exit(1)
		}
		parsedCutoverAt = parsedCutoverAt.UTC()
		observationCutoverAt = &parsedCutoverAt
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load community/shorts delivery-log config: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	options := opsapp.CommunityShortsDeliveryLogCollectOptions{
		ObservationRuntimeName:      strings.TrimSpace(*observationRuntime),
		ObservationBigBangCutoverAt: observationCutoverAt,
		Limit:                       *limit,
	}
	if !useObservationQuery {
		since := now.Add(-*window)
		options.Since = &since
	}

	report, err := opsapp.CollectCommunityShortsDeliveryLogReport(ctx, cfg, logger, now, options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to collect community/shorts delivery logs: %v\n", err)
		os.Exit(1)
	}

	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "markdown":
		if _, err := fmt.Print(opsapp.RenderCommunityShortsDeliveryLogMarkdown(report)); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write community/shorts delivery-log markdown: %v\n", err)
			os.Exit(1)
		}
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write community/shorts delivery-log JSON: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unsupported format %q (want markdown or json)\n", *format)
		os.Exit(1)
	}
}
