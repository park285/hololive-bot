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
	"github.com/kapu/hololive-stream-ingester/internal/app"
)

func main() {
	window := flag.Duration("window", 24*time.Hour, "lookback window for community/shorts per-channel delivery summary")
	format := flag.String("format", "markdown", "output format: markdown or json")
	flag.Parse()

	if *window <= 0 {
		fmt.Fprintln(os.Stderr, "window must be greater than zero")
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load community/shorts channel-summary config: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := app.CollectCommunityShortsChannelSummaryReport(ctx, cfg, logger, now, now.Add(-*window))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to collect community/shorts channel summary: %v\n", err)
		os.Exit(1)
	}

	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "markdown":
		if _, err := fmt.Print(app.RenderCommunityShortsChannelSummaryMarkdown(report)); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write community/shorts channel-summary markdown: %v\n", err)
			os.Exit(1)
		}
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write community/shorts channel-summary JSON: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unsupported format %q (want markdown or json)\n", *format)
		os.Exit(1)
	}
}
