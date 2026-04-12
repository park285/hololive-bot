package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	opsapp "github.com/kapu/hololive-stream-ingester/internal/ops"
)

func main() {
	window := flag.Duration("window", 24*time.Hour, "lookback window for actual delivery path evidence")
	flag.Parse()

	if *window <= 0 {
		fmt.Fprintln(os.Stderr, "window must be greater than zero")
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load community/shorts route verification config: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	report, err := opsapp.CollectCommunityShortsRouteVerificationReport(ctx, cfg, logger, now, now.Add(-*window))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to collect community/shorts route verification report: %v\n", err)
		os.Exit(1)
	}

	if _, err := fmt.Print(opsapp.RenderCommunityShortsRouteVerificationMarkdown(report)); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write community/shorts route verification report: %v\n", err)
		os.Exit(1)
	}
}
