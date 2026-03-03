package notification

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// SyncPlatformMappings: Rust 알람 서비스가 참조하는 플랫폼 매핑 해시를 현재 구독 채널 기준으로 동기화합니다.
// - alarm:chzzk_channels (youtube_channel_id -> chzzk_channel_id)
// - alarm:twitch_logins  (twitch_user_login -> youtube_channel_id)
func (as *AlarmService) SyncPlatformMappings(ctx context.Context) error {
	if as.cache == nil {
		return fmt.Errorf("cache service not configured")
	}
	if as.memberData == nil {
		return fmt.Errorf("member data provider not configured")
	}

	channelIDs, err := as.cache.SMembers(ctx, AlarmChannelRegistryKey)
	if err != nil {
		return fmt.Errorf("get channel registry: %w", err)
	}

	chzzkMappings := make(map[string]string, len(channelIDs))
	twitchMappings := make(map[string]string, len(channelIDs))

	for _, channelID := range channelIDs {
		member := as.memberData.FindMemberByChannelID(channelID)
		if member == nil {
			if as.logger != nil {
				as.logger.Warn("Skip platform mapping sync for unknown channel",
					slog.String("channel_id", channelID),
				)
			}
			continue
		}

		if chzzkChannelID := stringutil.TrimSpace(member.ChzzkChannelID); chzzkChannelID != "" {
			chzzkMappings[channelID] = chzzkChannelID
		}

		if twitchLogin := stringutil.Normalize(member.TwitchUserID); twitchLogin != "" {
			if existingChannelID, exists := twitchMappings[twitchLogin]; exists && existingChannelID != channelID {
				if as.logger != nil {
					as.logger.Warn("Duplicate Twitch login detected while syncing platform mappings",
						slog.String("twitch_login", twitchLogin),
						slog.String("kept_channel_id", existingChannelID),
						slog.String("ignored_channel_id", channelID),
					)
				}
				continue
			}
			twitchMappings[twitchLogin] = channelID
		}
	}

	if err := as.replaceHashMappings(ctx, ChzzkChannelMapKey, chzzkMappings); err != nil {
		return fmt.Errorf("sync chzzk channel mappings: %w", err)
	}

	if err := as.replaceHashMappings(ctx, TwitchLoginMapKey, twitchMappings); err != nil {
		return fmt.Errorf("sync twitch login mappings: %w", err)
	}

	if as.logger != nil {
		as.logger.Info("Platform alarm mappings synchronized",
			slog.Int("subscribed_channels", len(channelIDs)),
			slog.Int("chzzk_mappings", len(chzzkMappings)),
			slog.Int("twitch_mappings", len(twitchMappings)),
		)
	}

	return nil
}

func (as *AlarmService) replaceHashMappings(
	ctx context.Context,
	key string,
	mappings map[string]string,
) error {
	if err := as.cache.Del(ctx, key); err != nil {
		return fmt.Errorf("delete key %s: %w", key, err)
	}

	for field, value := range mappings {
		if err := as.cache.HSet(ctx, key, field, value); err != nil {
			return fmt.Errorf("hset key %s field %s: %w", key, field, err)
		}
	}

	return nil
}
