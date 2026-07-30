package alarmservice

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	sharedlogging "github.com/park285/shared-go/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/domain"
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
		return false, err
	}

	requestedTypes, err := normalizeAlarmTypesStrict(normalizedReq.AlarmTypes, domain.DefaultAlarmTypes)
	if err != nil {
		opErr = err
		return false, err
	}

	mutation, shouldAdd, err := as.prepareAddAlarmMutation(ctx, normalizedReq, requestedTypes)
	if err != nil {
		opErr = err
		return false, err
	}
	if !shouldAdd {
		return false, nil
	}

	if err := as.persistAddAlarmMutation(ctx, &mutation); err != nil {
		opErr = err
		return false, err
	}
	added, err := as.cacheAddAlarmMutation(ctx, &mutation)
	if err != nil {
		opErr = err
		return false, err
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
	return 0, sharedlogging.LogAndWrapError(ctx, as.logger, "rebuild add cache from repository", opErr)
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
		return nil, fmt.Errorf("room_id and channel_id are required")
	}
	return &normalized, nil
}

func (as *AlarmService) prepareAddAlarmMutation(ctx context.Context, req *domain.AddAlarmRequest, requestedTypes domain.AlarmTypes) (addAlarmMutation, bool, error) {
	existing, err := as.findAlarmRecordForMutation(ctx, req.RoomID, req.ChannelID)
	if err != nil {
		return addAlarmMutation{}, false, err
	}

	mergedTypes, newlyAddedTypes, err := addAlarmTypeMutation(existing, requestedTypes)
	if err != nil {
		return addAlarmMutation{}, false, err
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
		return nil, nil, err
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
		return sharedlogging.LogAndWrapError(ctx, as.logger, "persist alarm before cache write", err)
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
			slog.String("room_id", req.RoomID),
		)
	}
}
