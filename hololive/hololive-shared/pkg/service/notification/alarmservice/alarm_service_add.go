package alarmservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
)

func (as *AlarmService) AddAlarm(ctx context.Context, req *domain.AddAlarmRequest) (bool, error) {
	startedAt := time.Now()

	var opErr error

	defer func() {
		observeAlarmServiceOperation("add", startedAt, opErr)
	}()

	normalizedReq, err := normalizeAddAlarmRequest(req)
	if err != nil {
		opErr = err
		return false, fmt.Errorf("normalize add alarm request: %w", err)
	}

	requestedTypes, err := normalizeAlarmTypesStrict(normalizedReq.AlarmTypes, domain.DefaultAlarmTypes)
	if err != nil {
		opErr = err
		return false, fmt.Errorf("normalize alarm types strict: %w", err)
	}

	mutation, shouldAdd, err := as.prepareAddAlarmMutation(ctx, normalizedReq, requestedTypes)
	if err != nil {
		opErr = err
		return false, fmt.Errorf("prepare add alarm mutation: %w", err)
	}

	if !shouldAdd {
		return false, nil
	}

	if persistErr := as.persistAddAlarmMutation(ctx, &mutation); persistErr != nil {
		opErr = persistErr
		return false, fmt.Errorf("persist add alarm mutation: %w", persistErr)
	}

	added, err := as.cacheAddAlarmMutation(ctx, &mutation)
	if err != nil {
		opErr = err
		return false, fmt.Errorf("cache add alarm mutation: %w", err)
	}

	as.afterAddAlarm(ctx, normalizedReq, mutation.newlyAddedTypes)

	return added > 0 || mutation.existing, nil
}

func (as *AlarmService) cacheAddAlarmMutation(ctx context.Context, mutation *addAlarmMutation) (int64, error) {
	added, err := as.cacheAlarm(ctx, &mutation.cacheRecord)
	if err == nil {
		return added, nil
	}

	opErr := as.rebuildAlarmCacheFromRepository(ctx, "add", fmt.Errorf("add alarm: %w", err))

	if err := sharedlogging.LogAndWrapError(ctx, as.logger, "rebuild add cache from repository", opErr); err != nil {
		return 0, fmt.Errorf("log and wrap error: %w", err)
	}

	return 0, nil
}

func normalizeAddAlarmRequest(req *domain.AddAlarmRequest) (*domain.AddAlarmRequest, error) {
	normalized := *req

	normalized.RoomID = strings.TrimSpace(normalized.RoomID)
	normalized.UserID = strings.TrimSpace(normalized.UserID)
	normalized.ChannelID = strings.TrimSpace(normalized.ChannelID)
	normalized.MemberName = strings.TrimSpace(normalized.MemberName)
	normalized.RoomName = strings.TrimSpace(normalized.RoomName)
	normalized.UserName = strings.TrimSpace(normalized.UserName)

	if normalized.RoomID == "" || normalized.ChannelID == "" {
		return nil, errors.New("room_id and channel_id are required")
	}

	return &normalized, nil
}

func (as *AlarmService) prepareAddAlarmMutation(ctx context.Context, req *domain.AddAlarmRequest, requestedTypes domain.AlarmTypes) (addAlarmMutation, bool, error) {
	existing, err := as.findAlarmRecordForMutation(ctx, req.RoomID, req.ChannelID)
	if err != nil && !errors.Is(err, errAlarmRecordNotFound) {
		return addAlarmMutation{}, false, fmt.Errorf("find alarm record for mutation: %w", err)
	}

	mergedTypes, newlyAddedTypes, err := addAlarmTypeMutation(existing, requestedTypes)
	if err != nil {
		return addAlarmMutation{}, false, fmt.Errorf("add alarm type mutation: %w", err)
	}

	if len(newlyAddedTypes) == 0 {
		return addAlarmMutation{}, false, nil
	}

	record := buildAlarmRecord(req, mergedTypes)
	cacheRecord := *record

	if existing != nil {
		cacheRecord.AlarmTypes = newlyAddedTypes
	}

	return addAlarmMutation{record: record, cacheRecord: cacheRecord, newlyAddedTypes: newlyAddedTypes, existing: existing != nil}, true, nil
}

func addAlarmTypeMutation(existing *domain.Alarm, requestedTypes domain.AlarmTypes) (merged, newlyAdded domain.AlarmTypes, err error) {
	if existing == nil {
		return requestedTypes, requestedTypes, nil
	}

	existingTypes, err := normalizeAlarmTypesStrict(existing.AlarmTypes, domain.DefaultAlarmTypes)
	if err != nil {
		return nil, nil, fmt.Errorf("normalize alarm types strict: %w", err)
	}

	mergedTypes := mergeAlarmTypes(existingTypes, requestedTypes)

	return mergedTypes, subtractAlarmTypes(mergedTypes, existingTypes), nil
}

func (as *AlarmService) persistAddAlarmMutation(ctx context.Context, mutation *addAlarmMutation) error {
	var err error

	if mutation.existing {
		err = as.updateAlarmTypes(ctx, mutation.record)
	} else {
		err = as.persistAlarm(ctx, mutation.record)
	}

	if err != nil {
		if logErr := sharedlogging.LogAndWrapError(ctx, as.logger, "persist alarm before cache write", err); logErr != nil {
			return fmt.Errorf("log and wrap error: %w", logErr)
		}

		return nil
	}

	return nil
}

func (as *AlarmService) afterAddAlarm(ctx context.Context, req *domain.AddAlarmRequest, newlyAddedTypes domain.AlarmTypes) {
	as.logAlarmAdded(req, newlyAddedTypes)

	if syncErr := as.syncPlatformMappingForChannel(ctx, req.ChannelID); syncErr != nil && as.logger != nil {
		sharedlogging.LogWarnWithErrorAttrs(ctx, as.logger,
			"sync platform alarm mapping after add.failed",
			"Failed to sync platform alarm mapping after add",
			syncErr,
			slog.String("channel_id", req.ChannelID),
			privacylog.RoomIDAttr(req.RoomID),
		)
	}
}
