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
	"strings"
	"syscall"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
)

const activationTimeout = 30 * time.Second

type commandOptions struct {
	activatedBy string
	reason      string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	os.Exit(run(ctx, os.Args[1:], os.Stderr))
}

func run(ctx context.Context, args []string, stderr io.Writer) int {
	options, err := parseCommandOptions(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}

		if _, writeErr := fmt.Fprintf(stderr, "parse source observation replay epoch options: %v\n", err); writeErr != nil {
			return 1
		}

		return 2
	}

	logger := slog.New(slog.NewJSONHandler(stderr, nil))

	config, err := settings.LoadBotRuntime()
	if err != nil {
		logger.Error("load hololive API runtime config", slog.Any("error", err))

		return 1
	}

	postgres := config.Postgres

	postgres.PoolMinConns = 0
	postgres.PoolMaxConns = 1

	activationContext, cancel := context.WithTimeout(ctx, activationTimeout)
	defer cancel()

	resources, cleanup, err := providers.ProvideDatabaseResources(activationContext, &postgres, logger)
	if err != nil {
		logger.Error("open source observation replay epoch database", slog.Any("error", err))

		return 1
	}
	defer cleanup()

	result, err := sourceobservation.NewRepository(resources.Service.GetPool()).ActivateReplayEpoch(
		activationContext,
		sourceobservation.ReplayEpochInput{
			ActivatedBy: options.activatedBy,
			Reason:      options.reason,
		},
	)
	if err != nil {
		logger.Error("activate source observation replay epoch", slog.Any("error", err))

		return 1
	}

	logger.Info(
		"source observation replay epoch ready",
		slog.Bool("activated", result.Activated),
		slog.Time("cutoff_received_at", result.Epoch.CutoffReceivedAt),
		slog.String("activated_by", result.Epoch.ActivatedBy),
	)

	return 0
}

func parseCommandOptions(args []string, stderr io.Writer) (commandOptions, error) {
	flags := flag.NewFlagSet("source-observation-replay-epoch", flag.ContinueOnError)
	flags.SetOutput(stderr)

	activatedBy := flags.String("activated-by", "", "operator identity recorded with the immutable epoch")
	reason := flags.String("reason", "", "reason recorded with the immutable epoch")

	if err := flags.Parse(args); err != nil {
		return commandOptions{}, fmt.Errorf("parse command flags: %w", err)
	}

	if flags.NArg() != 0 {
		return commandOptions{}, fmt.Errorf("unexpected positional arguments: %d", flags.NArg())
	}

	if err := validateFlag("activated-by", *activatedBy, 128); err != nil {
		return commandOptions{}, fmt.Errorf("validate activated-by: %w", err)
	}

	if err := validateFlag("reason", *reason, 1024); err != nil {
		return commandOptions{}, fmt.Errorf("validate reason: %w", err)
	}

	return commandOptions{activatedBy: *activatedBy, reason: *reason}, nil
}

func validateFlag(name, value string, maxBytes int) error {
	if value == "" {
		return fmt.Errorf("--%s is required", name)
	}

	if strings.TrimSpace(value) != value {
		return fmt.Errorf("--%s must not contain surrounding whitespace", name)
	}

	if len(value) > maxBytes {
		return fmt.Errorf("--%s exceeds %d bytes", name, maxBytes)
	}

	return nil
}
