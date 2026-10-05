package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/privacylog"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

// 호출자는 mutation mutex를 보유한다. 빈 구독 표식을 먼저 지워 positive 삭제 뒤
// 기존 DB read-through가 가려지지 않게 하고, 모두 성공한 뒤에만 DB 변경을 시작한다.
func (as *AlarmService) invalidateChannelSubscribers(ctx context.Context, channelID string, alarmTypes domain.AlarmTypes) error {
	for _, alarmType := range alarmTypes {
		if err := as.cache.Del(ctx, sharedalarmkeys.BuildChannelSubscriberEmptyKey(channelID, alarmType)); err != nil {
			return fmt.Errorf("invalidate subscriber empty marker: type %s: %w", alarmType, err)
		}
	}

	for _, alarmType := range alarmTypes {
		if err := as.cache.Del(ctx, as.channelSubscribersKeyByType(channelID, alarmType)); err != nil {
			return fmt.Errorf("invalidate subscriber set: type %s: %w", alarmType, err)
		}
	}

	return nil
}

func (as *AlarmService) prepareAddedAlarmCache(ctx context.Context, channelID string, alarmTypes domain.AlarmTypes) error {
	// 새 채널은 먼저 발견 가능하게 만든다. commit 전에는 DB 구독이 없으므로 수신 방이 생기지 않는다.
	if err := as.cacheAlarmChannelRegistry(ctx, channelID); err != nil {
		return fmt.Errorf("register channel before commit: %w", err)
	}

	if err := as.markAlarmCacheChanged(ctx); err != nil {
		return fmt.Errorf("clear empty registry marker before commit: %w", err)
	}

	if err := as.invalidateChannelSubscribers(ctx, channelID, alarmTypes); err != nil {
		return fmt.Errorf("invalidate subscribers before commit: %w", err)
	}

	return nil
}

func normalizeAlarmTypesStrict(input, fallback domain.AlarmTypes) (domain.AlarmTypes, error) {
	if len(input) == 0 {
		input = fallback
	}

	valid := alarmTypeSet(domain.AllAlarmTypes)
	seen := make(map[domain.AlarmType]struct{}, len(input))
	normalized := make(domain.AlarmTypes, 0, len(input))

	for _, alarmType := range input {
		var err error

		normalized, err = appendNormalizedAlarmType(normalized, seen, alarmType, valid)
		if err != nil {
			return nil, fmt.Errorf("append normalized alarm type: %w", err)
		}
	}

	if len(normalized) == 0 {
		return nil, errors.New("alarm types are empty after normalization")
	}

	return normalized, nil
}

func appendNormalizedAlarmType(normalized domain.AlarmTypes, seen map[domain.AlarmType]struct{}, alarmType domain.AlarmType, valid map[domain.AlarmType]struct{}) (domain.AlarmTypes, error) {
	trimmed, ok, err := normalizeAlarmTypeStrict(alarmType, valid)
	if err != nil {
		return nil, fmt.Errorf("normalize alarm type strict: %w", err)
	}

	if !ok {
		return normalized, nil
	}

	if _, ok := seen[trimmed]; ok {
		return normalized, nil
	}

	seen[trimmed] = struct{}{}

	return append(normalized, trimmed), nil
}

func normalizeAlarmTypeStrict(alarmType domain.AlarmType, valid map[domain.AlarmType]struct{}) (domain.AlarmType, bool, error) {
	trimmed := domain.AlarmType(strings.TrimSpace(string(alarmType)))
	if trimmed == "" {
		return "", false, nil
	}

	if _, ok := valid[trimmed]; !ok {
		return "", false, fmt.Errorf("unknown alarm type: %s", alarmType)
	}

	return trimmed, true, nil
}

func alarmTypeSet(types domain.AlarmTypes) map[domain.AlarmType]struct{} {
	result := make(map[domain.AlarmType]struct{}, len(types))
	for _, alarmType := range types {
		result[alarmType] = struct{}{}
	}

	return result
}

func mergeAlarmTypes(existing, requested domain.AlarmTypes) domain.AlarmTypes {
	seen := alarmTypeSet(existing)
	merged := make(domain.AlarmTypes, 0, len(existing)+len(requested))

	merged = append(merged, existing...)

	for _, alarmType := range requested {
		if _, ok := seen[alarmType]; ok {
			continue
		}

		seen[alarmType] = struct{}{}
		merged = append(merged, alarmType)
	}

	return merged
}

func subtractAlarmTypes(existing, remove domain.AlarmTypes) domain.AlarmTypes {
	removeSet := alarmTypeSet(remove)
	result := make(domain.AlarmTypes, 0, len(existing))

	for _, alarmType := range existing {
		if _, shouldRemove := removeSet[alarmType]; shouldRemove {
			continue
		}

		result = append(result, alarmType)
	}

	return result
}

func intersectAlarmTypes(existing, requested domain.AlarmTypes) domain.AlarmTypes {
	existingSet := alarmTypeSet(existing)
	seen := make(map[domain.AlarmType]struct{}, len(requested))
	result := make(domain.AlarmTypes, 0, len(requested))

	for _, alarmType := range requested {
		if _, duplicated := seen[alarmType]; duplicated {
			continue
		}

		seen[alarmType] = struct{}{}
		if _, ok := existingSet[alarmType]; ok {
			result = append(result, alarmType)
		}
	}

	return result
}

func buildAlarmRecord(req *domain.AddAlarmRequest, alarmTypes domain.AlarmTypes) *domain.Alarm {
	return &domain.Alarm{
		RoomID:     req.RoomID,
		UserID:     req.UserID,
		ChannelID:  req.ChannelID,
		HostID:     req.HostID,
		RoomName:   req.RoomName,
		UserName:   req.UserName,
		AlarmTypes: alarmTypes,
	}
}

func (as *AlarmService) logAlarmAdded(req *domain.AddAlarmRequest, alarmTypes domain.AlarmTypes) {
	if as.logger == nil {
		return
	}

	as.logger.Info("Alarm added",
		privacylog.RoomIDAttr(req.RoomID),
		slog.String("channel_id", req.ChannelID),
		slog.Any("alarm_types", alarmTypes),
	)
}

func (as *AlarmService) cleanupChannelRegistryIfEmpty(ctx context.Context, channelID string) error {
	hasSubscribers, err := as.alarmRepository.HasChannelSubscriptions(ctx, channelID)
	if err != nil {
		return fmt.Errorf("check authoritative channel subscriptions: %w", err)
	}

	if hasSubscribers {
		return as.cacheAlarmChannelRegistry(ctx, channelID)
	}

	if _, err := as.cache.SRem(ctx, sharedalarmkeys.AlarmChannelRegistryKey, []string{channelID}); err != nil {
		return fmt.Errorf("cleanup channel registry: remove channel registry entry: %w", err)
	}

	return nil
}
