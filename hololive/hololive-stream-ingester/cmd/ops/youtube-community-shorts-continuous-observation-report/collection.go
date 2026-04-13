package main

import (
	"context"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	opsapp "github.com/kapu/hololive-stream-ingester/internal/ops"
	"log/slog"
)

const continuousObservationRetryInterval = 5 * time.Second

type continuousObservationOutputPaths struct {
	latest   string
	snapshot string
}

func collectContinuousObservationWithWait(
	cfg *config.Config,
	logger *slog.Logger,
	options opsapp.CommunityShortsContinuousObservationCollectOptions,
	waitTimeout time.Duration,
) (opsapp.CommunityShortsContinuousObservationReport, error) {
	deadline := time.Now().Add(waitTimeout)
	for {
		report, err := collectContinuousObservationOnce(cfg, logger, options)
		if err == nil {
			return report, nil
		}
		if time.Now().After(deadline) || !isObservationWindowNotFoundError(err) {
			return opsapp.CommunityShortsContinuousObservationReport{}, err
		}
		time.Sleep(continuousObservationRetryInterval)
	}
}

func collectContinuousObservationOnce(
	cfg *config.Config,
	logger *slog.Logger,
	options opsapp.CommunityShortsContinuousObservationCollectOptions,
) (opsapp.CommunityShortsContinuousObservationReport, error) {
	now := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return opsapp.CollectCommunityShortsContinuousObservationReport(ctx, cfg, logger, now, options)
}

func nextContinuousObservationInterval(report opsapp.CommunityShortsContinuousObservationReport) time.Duration {
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

func isObservationWindowNotFoundError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "observation window not found")
}
