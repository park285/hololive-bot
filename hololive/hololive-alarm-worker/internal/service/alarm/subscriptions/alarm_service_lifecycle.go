package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	alarmServiceCloseTimeout = 3 * time.Second
	alarmPersistTaskTimeout  = alarmServiceCloseTimeout
)

func NewAlarmService(
	cacheClient Cache,
	memberData domain.MemberDataProvider,
	alarmRepository *alarmstore.Repository,
	logger *slog.Logger,
	advanceMinutes []int,
) (*AlarmService, error) {
	if cacheClient == nil {
		return nil, errors.New("new alarm service: cache client is nil")
	}

	if alarmRepository == nil {
		return nil, errors.New("new alarm service: alarm repository is nil")
	}

	if logger == nil {
		logger = slog.Default()
	}

	alarmMetrics()

	targetPolicy := targetpolicy.NewTargetMinutePolicy(targetpolicy.NormalizeTargetMinutes(advanceMinutes))

	return &AlarmService{
		cache:           cacheClient,
		memberData:      memberData,
		alarmRepository: alarmRepository,
		alarmWriter:     alarmRepository,
		logger:          logger,
		targetPolicy:    targetPolicy,
	}, nil
}

func (as *AlarmService) getTargetMinutes() []int {
	as.targetMinutesMu.RLock()
	defer as.targetMinutesMu.RUnlock()

	return as.targetPolicy.Clone()
}

func (as *AlarmService) GetTargetMinutes() []int {
	return as.getTargetMinutes()
}

func (as *AlarmService) UpdateAlarmAdvanceMinutes(ctx context.Context, alarmAdvanceMinutes int) (domain.AdvanceMinutesResult, error) {
	result := domain.AdvanceMinutesResult{RequestedMinutes: alarmAdvanceMinutes, Outcome: domain.ApplyRejected}

	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("update alarm advance minutes: before apply: %w", err)
	}

	normalized := targetpolicy.NewTargetMinutePolicyFromRuntimeAdvance(alarmAdvanceMinutes)

	as.targetMinutesMu.Lock()

	if err := ctx.Err(); err != nil {
		as.targetMinutesMu.Unlock()

		return result, fmt.Errorf("update alarm advance minutes: before apply: %w", err)
	}

	as.targetPolicy = normalized
	as.targetMinutesMu.Unlock()

	if as.logger != nil {
		as.logger.Info("Alarm advance minutes updated",
			slog.Int("alarm_advance_minutes", alarmAdvanceMinutes),
			slog.Group("target_minutes", slog.Int("primary", normalized.PrimaryAdvanceMinute())),
		)
	}

	result.Outcome = domain.ApplyConfirmed
	result.TargetMinutes = normalized.Clone()

	return result, nil
}
