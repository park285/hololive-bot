package notification

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/valkey-io/valkey-go"
)

func (as *AlarmService) removeChannelSubscribers(ctx context.Context, channelID, registryKey string, alarmTypes domain.AlarmTypes) error {
	if len(alarmTypes) == 0 {
		return nil
	}

	builder := as.cache.Builder()
	subscriberKeys := as.channelSubscriberKeys(channelID, alarmTypes)
	if err := as.executeSubscriberTypeRemoval(ctx, builder, subscriberKeys, registryKey, alarmTypes); err != nil {
		return fmt.Errorf("execute subscriber type removal: %w", err)
	}

	cleanupKeys, err := as.collectEmptySubscriberKeys(ctx, builder, subscriberKeys, alarmTypes, "remove channel subscribers")
	if err != nil {
		return fmt.Errorf("collect empty subscriber keys: %w", err)
	}

	if err := as.deleteSubscriberKeys(ctx, builder, cleanupKeys, "remove channel subscribers"); err != nil {
		return fmt.Errorf("delete subscriber keys: %w", err)
	}

	return nil
}

func (as *AlarmService) clearChannelSubscribersPipeline(ctx context.Context, alarms []string, registryKey string) error {
	if len(alarms) == 0 {
		return nil
	}

	builder := as.cache.Builder()
	channelSubsKeys := as.roomChannelSubscriberKeys(alarms)
	if err := as.executeSubscriberKeyRemoval(ctx, builder, channelSubsKeys, registryKey, "clear channel subscribers"); err != nil {
		return fmt.Errorf("execute subscriber key removal: %w", err)
	}

	cleanupKeys, err := as.collectEmptySubscriberKeys(ctx, builder, channelSubsKeys, nil, "clear channel subscribers")
	if err != nil {
		return fmt.Errorf("collect empty subscriber keys: %w", err)
	}

	if err := as.deleteSubscriberKeys(ctx, builder, cleanupKeys, "clear channel subscribers"); err != nil {
		return fmt.Errorf("delete subscriber keys: %w", err)
	}

	return nil
}

func normalizeAlarmTypesStrict(input domain.AlarmTypes, fallback domain.AlarmTypes) (domain.AlarmTypes, error) {
	if len(input) == 0 {
		input = fallback
	}

	valid := make(map[domain.AlarmType]struct{}, len(domain.AllAlarmTypes))
	for _, alarmType := range domain.AllAlarmTypes {
		valid[alarmType] = struct{}{}
	}

	seen := make(map[domain.AlarmType]struct{}, len(input))
	normalized := make(domain.AlarmTypes, 0, len(input))
	for _, alarmType := range input {
		trimmed := domain.AlarmType(strings.TrimSpace(string(alarmType)))
		if trimmed == "" {
			continue
		}
		if _, ok := valid[trimmed]; !ok {
			return nil, fmt.Errorf("unknown alarm type: %s", alarmType)
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	if len(normalized) == 0 {
		return nil, fmt.Errorf("alarm types are empty after normalization")
	}

	return normalized, nil
}

func alarmTypeSet(types domain.AlarmTypes) map[domain.AlarmType]struct{} {
	result := make(map[domain.AlarmType]struct{}, len(types))
	for _, alarmType := range types {
		result[alarmType] = struct{}{}
	}
	return result
}

func mergeAlarmTypes(existing domain.AlarmTypes, requested domain.AlarmTypes) domain.AlarmTypes {
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

func subtractAlarmTypes(existing domain.AlarmTypes, remove domain.AlarmTypes) domain.AlarmTypes {
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

func intersectAlarmTypes(existing domain.AlarmTypes, requested domain.AlarmTypes) domain.AlarmTypes {
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

func buildAlarmRecord(req domain.AddAlarmRequest, alarmTypes domain.AlarmTypes) *domain.Alarm {
	return &domain.Alarm{
		RoomID:     req.RoomID,
		UserID:     req.UserID,
		ChannelID:  req.ChannelID,
		MemberName: req.MemberName,
		RoomName:   req.RoomName,
		UserName:   req.UserName,
		AlarmTypes: alarmTypes,
	}
}

func (as *AlarmService) logAlarmAdded(req domain.AddAlarmRequest, alarmTypes domain.AlarmTypes) {
	if as.logger == nil {
		return
	}

	as.logger.Info("Alarm added",
		slog.String("room_id", req.RoomID),
		slog.String("room_name", req.RoomName),
		slog.String("user_id", req.UserID),
		slog.String("user_name", req.UserName),
		slog.String("channel_id", req.ChannelID),
		slog.String("member_name", req.MemberName),
		slog.Any("alarm_types", alarmTypes),
	)
}

func (as *AlarmService) channelSubscriberKeys(channelID string, alarmTypes domain.AlarmTypes) []string {
	keys := make([]string, len(alarmTypes))
	for i, alarmType := range alarmTypes {
		keys[i] = as.channelSubscribersKeyByType(channelID, alarmType)
	}

	return keys
}

func (as *AlarmService) roomChannelSubscriberKeys(channelIDs []string) []string {
	keys := make([]string, 0, len(channelIDs)*len(domain.AllAlarmTypes))
	for _, channelID := range channelIDs {
		keys = append(keys, as.channelSubscriberKeys(channelID, domain.AllAlarmTypes)...)
	}

	return keys
}

func (as *AlarmService) executeSubscriberTypeRemoval(ctx context.Context, builder valkey.Builder, subscriberKeys []string, registryKey string, alarmTypes domain.AlarmTypes) error {
	results := as.cache.DoMulti(ctx, buildSubscriberSRemCommands(builder, subscriberKeys, registryKey)...)
	if len(results) != len(subscriberKeys) {
		return fmt.Errorf("remove channel subscribers: unexpected SREM result count: %d", len(results))
	}

	for i, result := range results {
		if err := result.Error(); err != nil {
			return fmt.Errorf("remove channel subscribers: srem type %s: %w", alarmTypes[i], err)
		}
	}

	return nil
}

func (as *AlarmService) executeSubscriberKeyRemoval(ctx context.Context, builder valkey.Builder, subscriberKeys []string, registryKey string, operation string) error {
	results := as.cache.DoMulti(ctx, buildSubscriberSRemCommands(builder, subscriberKeys, registryKey)...)
	if len(results) != len(subscriberKeys) {
		return fmt.Errorf("%s: unexpected SREM result count: %d", operation, len(results))
	}

	for i, result := range results {
		if err := result.Error(); err != nil {
			return fmt.Errorf("%s: srem key %s: %w", operation, subscriberKeys[i], err)
		}
	}

	return nil
}

func buildSubscriberSRemCommands(builder valkey.Builder, subscriberKeys []string, registryKey string) []valkey.Completed {
	commands := make([]valkey.Completed, len(subscriberKeys))
	for i, key := range subscriberKeys {
		commands[i] = builder.Srem().Key(key).Member(registryKey).Build()
	}

	return commands
}

func (as *AlarmService) collectEmptySubscriberKeys(ctx context.Context, builder valkey.Builder, subscriberKeys []string, alarmTypes domain.AlarmTypes, operation string) ([]string, error) {
	scardCommands := make([]valkey.Completed, len(subscriberKeys))
	for i, key := range subscriberKeys {
		scardCommands[i] = builder.Scard().Key(key).Build()
	}

	results := as.cache.DoMulti(ctx, scardCommands...)
	if len(results) != len(scardCommands) {
		return nil, fmt.Errorf("%s: unexpected SCARD result count: %d", operation, len(results))
	}

	cleanupKeys := make([]string, 0, len(results))
	for i, result := range results {
		count, err := result.AsInt64()
		if err != nil {
			if len(alarmTypes) > 0 {
				return nil, fmt.Errorf("%s: scard type %s: %w", operation, alarmTypes[i], err)
			}

			return nil, fmt.Errorf("%s: scard key %s: %w", operation, subscriberKeys[i], err)
		}

		if count == 0 {
			cleanupKeys = append(cleanupKeys, subscriberKeys[i])
		}
	}

	return cleanupKeys, nil
}

func (as *AlarmService) deleteSubscriberKeys(ctx context.Context, builder valkey.Builder, cleanupKeys []string, operation string) error {
	if len(cleanupKeys) == 0 {
		return nil
	}

	cleanupCommands := make([]valkey.Completed, len(cleanupKeys))
	for i, key := range cleanupKeys {
		cleanupCommands[i] = builder.Del().Key(key).Build()
	}

	results := as.cache.DoMulti(ctx, cleanupCommands...)
	if len(results) != len(cleanupCommands) {
		return fmt.Errorf("%s: unexpected DEL result count: %d", operation, len(results))
	}

	for i, result := range results {
		if err := result.Error(); err != nil {
			return fmt.Errorf("%s: delete key %s: %w", operation, cleanupKeys[i], err)
		}
	}

	return nil
}

func (as *AlarmService) cleanupChannelRegistryIfEmpty(ctx context.Context, channelID string) error {
	builder := as.cache.Builder()

	allSubsKeys := make([]string, 0, len(domain.AllAlarmTypes))
	for _, alarmType := range domain.AllAlarmTypes {
		allSubsKeys = append(allSubsKeys, as.channelSubscribersKeyByType(channelID, alarmType))
	}

	scardCmds := make([]valkey.Completed, 0, len(allSubsKeys))
	for _, key := range allSubsKeys {
		scardCmds = append(scardCmds, builder.Scard().Key(key).Build())
	}

	scardResults := as.cache.DoMulti(ctx, scardCmds...)
	if len(scardResults) != len(scardCmds) {
		return fmt.Errorf("cleanup channel registry: unexpected SCARD result count: %d", len(scardResults))
	}

	for i, res := range scardResults {
		count, err := res.AsInt64()
		if err != nil {
			return fmt.Errorf("cleanup channel registry: scard key %s: %w", allSubsKeys[i], err)
		}
		if count > 0 {
			return nil
		}
	}

	if _, err := as.cache.SRem(ctx, AlarmChannelRegistryKey, []string{channelID}); err != nil {
		return fmt.Errorf("cleanup channel registry: remove channel registry entry: %w", err)
	}

	return nil
}
