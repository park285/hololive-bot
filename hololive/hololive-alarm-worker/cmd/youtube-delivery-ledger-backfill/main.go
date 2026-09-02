package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/backfill"
	"github.com/kapu/hololive-shared/pkg/config/settings/alarmworker"
	"github.com/kapu/hololive-shared/pkg/providers"
)

type commandOptions struct {
	backfill.Options
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	os.Exit(run(ctx, os.Args[1:], os.Stderr))
}

func run(ctx context.Context, args []string, stderr io.Writer) int {
	options, err := parseBackfillOptions(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}

		if _, writeErr := fmt.Fprintf(stderr, "parse delivery ledger backfill options: %v\n", err); writeErr != nil {
			return 1
		}

		return 2
	}

	logger := slog.New(slog.NewJSONHandler(stderr, nil))

	config, err := alarmworker.LoadRuntime()
	if err != nil {
		logger.Error("load alarm-worker runtime config", slog.Any("error", err))

		return 1
	}

	resources, cleanup, err := providers.ProvideDatabaseResources(ctx, &config.Postgres, logger)
	if err != nil {
		logger.Error("open delivery ledger backfill database", slog.Any("error", err))

		return 1
	}
	defer cleanup()

	runner, err := backfill.New(resources.Service.GetPool(), options.Options)
	if err != nil {
		logger.Error("configure delivery ledger backfill", slog.Any("error", err))

		return 1
	}

	result, err := runner.Run(ctx)
	if err != nil {
		logger.Error("run delivery ledger backfill", slog.Any("error", err))

		return 1
	}

	logger.Info(
		"delivery ledger backfill run finished",
		slog.Int64("delivery_high_water_id", result.State.DeliveryHighWaterID),
		slog.Int64("delivery_cursor_id", result.State.DeliveryCursorID),
		slog.Int64("delivery_verify_cursor_id", result.State.DeliveryVerifyCursorID),
		slog.Int64("outbox_high_water_id", result.State.OutboxHighWaterID),
		slog.Int64("outbox_cursor_id", result.State.OutboxCursorID),
		slog.Bool("completed", result.Completed),
	)

	return 0
}

func parseBackfillOptions(args []string, stderr io.Writer) (commandOptions, error) {
	flags := flag.NewFlagSet("youtube-delivery-ledger-backfill", flag.ContinueOnError)
	flags.SetOutput(stderr)

	batchSize := flags.Int("batch-size", backfill.DefaultBatchSize, "rows processed per transaction")
	coverageStart := flags.String(
		"legacy-coverage-start-at",
		"",
		"earliest independently verified replay floor in RFC3339 format",
	)
	confirmCoverage := flags.Bool(
		"confirm-historical-coverage",
		false,
		"confirm that legacy coverage was independently verified",
	)

	if err := flags.Parse(args); err != nil {
		return commandOptions{}, fmt.Errorf("parse command flags: %w", err)
	}

	if flags.NArg() != 0 {
		return commandOptions{}, fmt.Errorf("unexpected positional arguments: %d", flags.NArg())
	}

	options := commandOptions{
		BatchSize:                 *batchSize,
		HistoricalCoverageChecked: *confirmCoverage,
	}

	if *coverageStart != "" {
		parsed, err := time.Parse(time.RFC3339, *coverageStart)
		if err != nil {
			return commandOptions{}, fmt.Errorf("parse legacy coverage start: %w", err)
		}

		parsed = parsed.UTC()
		options.LegacyCoverageStartAt = &parsed
	}

	if options.HistoricalCoverageChecked != (options.LegacyCoverageStartAt != nil) {
		return commandOptions{}, backfill.ErrCoverageConfirmationRequired
	}

	return options, nil
}
