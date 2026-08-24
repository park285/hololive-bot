package platformmap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/logging"
	"github.com/park285/shared-go/v2/pkg/stringutil"

	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

func (m *Mapper) reconcileDesiredTwitchLoginMapping(ctx context.Context, channelID, desiredLogin string) error {
	existingChannelID, err := m.cache.HGet(ctx, sharedalarmkeys.TwitchLoginMapKey, desiredLogin)
	if err != nil {
		return fmt.Errorf("get desired twitch login mapping: %w", err)
	}

	if existingChannelID != "" && existingChannelID != channelID {
		if err := m.clearConflictingTwitchChannelLoginMapping(ctx, desiredLogin, existingChannelID, channelID); err != nil {
			return fmt.Errorf("clear conflicting twitch channel login mapping: %w", err)
		}

		return nil
	}

	if err := m.upsertTwitchMappingsForChannel(ctx, channelID, desiredLogin); err != nil {
		return fmt.Errorf("upsert twitch mappings for channel: %w", err)
	}

	return nil
}

func (m *Mapper) currentTwitchChannelLogin(ctx context.Context, channelID string) (string, error) {
	currentLogin, err := m.cache.HGet(ctx, sharedalarmkeys.TwitchChannelLoginMapKey, channelID)
	if err != nil {
		return "", fmt.Errorf("get current twitch channel login: %w", err)
	}

	return stringutil.Normalize(currentLogin), nil
}

func (m *Mapper) removeChangedTwitchLoginMapping(
	ctx context.Context,
	currentLogin string,
	channelID string,
	desiredLogin string,
) error {
	if currentLogin != "" && currentLogin != desiredLogin {
		if err := m.removeStaleTwitchLoginMappingIfOwned(ctx, currentLogin, channelID); err != nil {
			return fmt.Errorf("delete stale twitch login mapping: %w", err)
		}
	}

	if desiredLogin == "" {
		if err := m.deleteTwitchChannelLoginMapping(ctx, channelID); err != nil {
			return fmt.Errorf("delete twitch channel login mapping: %w", err)
		}

		return nil
	}

	return nil
}

func (m *Mapper) deleteTwitchChannelLoginMapping(ctx context.Context, channelID string) error {
	if err := m.cache.HDel(ctx, sharedalarmkeys.TwitchChannelLoginMapKey, channelID); err != nil {
		return fmt.Errorf("delete twitch channel login mapping: %w", err)
	}

	return nil
}

func (m *Mapper) clearConflictingTwitchChannelLoginMapping(
	ctx context.Context,
	desiredLogin string,
	existingChannelID string,
	channelID string,
) error {
	if err := m.cache.HDel(ctx, sharedalarmkeys.TwitchChannelLoginMapKey, existingChannelID); err != nil {
		return fmt.Errorf("prune stale twitch channel login mapping: %w", err)
	}

	logging.Warn(ctx, m.logger, "platform_mapping.duplicate_twitch_login", "Duplicate Twitch login detected while incrementally syncing platform mappings",
		slog.String("twitch_login", desiredLogin),
		slog.String("pruned_channel_id", existingChannelID),
		slog.String("owner_channel_id", channelID),
	)

	if err := m.upsertTwitchMappingsForChannel(ctx, channelID, desiredLogin); err != nil {
		return fmt.Errorf("upsert twitch mappings for channel: %w", err)
	}

	return nil
}

func (m *Mapper) upsertTwitchMappingsForChannel(ctx context.Context, channelID, desiredLogin string) error {
	if err := m.cache.HSet(ctx, sharedalarmkeys.TwitchLoginMapKey, desiredLogin, channelID); err != nil {
		return fmt.Errorf("upsert twitch mapping: %w", err)
	}

	if err := m.cache.Del(ctx, sharedalarmkeys.TwitchLoginMapEmptyKey); err != nil {
		return fmt.Errorf("clear twitch empty marker: %w", err)
	}

	if err := m.cache.HSet(ctx, sharedalarmkeys.TwitchChannelLoginMapKey, channelID, desiredLogin); err != nil {
		return fmt.Errorf("upsert twitch channel login mapping: %w", err)
	}

	if err := m.cache.Del(ctx, sharedalarmkeys.TwitchChannelLoginMapEmptyKey); err != nil {
		return fmt.Errorf("clear twitch channel empty marker: %w", err)
	}

	return nil
}
