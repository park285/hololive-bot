// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package notification

import (
	"errors"
	"context"
	"fmt"
	"log/slog"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// - alarm:chzzk_channels (youtube_channel_id -> chzzk_channel_id)
// - alarm:twitch_logins  (twitch_user_login -> youtube_channel_id)
// - alarm:twitch_channel_logins (youtube_channel_id -> twitch_user_login).
func (as *AlarmService) SyncPlatformMappings(ctx context.Context) error {
	if as.cache == nil {
		return errors.New("cache service not configured")
	}

	if as.memberData == nil {
		return errors.New("member data provider not configured")
	}

	as.platformMapMu.Lock()
	defer as.platformMapMu.Unlock()

	channelIDs, err := as.cache.SMembers(ctx, AlarmChannelRegistryKey)
	if err != nil {
		return fmt.Errorf("get channel registry: %w", err)
	}

	chzzkMappings := make(map[string]string, len(channelIDs))
	twitchMappings := make(map[string]string, len(channelIDs))
	twitchChannelMappings := make(map[string]string, len(channelIDs))

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
			twitchChannelMappings[channelID] = twitchLogin
		}
	}

	if err := as.replaceHashMappings(ctx, ChzzkChannelMapKey, chzzkMappings); err != nil {
		return fmt.Errorf("sync chzzk channel mappings: %w", err)
	}

	if err := as.replaceHashMappings(ctx, TwitchLoginMapKey, twitchMappings); err != nil {
		return fmt.Errorf("sync twitch login mappings: %w", err)
	}

	if err := as.replaceHashMappings(ctx, TwitchChannelLoginMapKey, twitchChannelMappings); err != nil {
		return fmt.Errorf("sync twitch channel login mappings: %w", err)
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

func (as *AlarmService) syncPlatformMappingForChannel(ctx context.Context, channelID string) error {
	if as.cache == nil {
		return errors.New("cache service not configured")
	}

	if as.memberData == nil {
		return errors.New("member data provider not configured")
	}

	as.platformMapMu.Lock()
	defer as.platformMapMu.Unlock()

	registered, err := as.cache.SIsMember(ctx, AlarmChannelRegistryKey, channelID)
	if err != nil {
		return fmt.Errorf("check channel registry membership: %w", err)
	}

	if !registered {
		if err := as.removePlatformMappingsForChannel(ctx, channelID); err != nil {
			return fmt.Errorf("remove stale platform mappings: %w", err)
		}

		return nil
	}

	member := as.memberData.FindMemberByChannelID(channelID)
	if member == nil {
		if as.logger != nil {
			as.logger.Warn("Skip platform mapping update for unknown channel",
				slog.String("channel_id", channelID),
			)
		}

		if err := as.removePlatformMappingsForChannel(ctx, channelID); err != nil {
			return fmt.Errorf("remove unknown channel platform mappings: %w", err)
		}

		return nil
	}

	if chzzkChannelID := stringutil.TrimSpace(member.ChzzkChannelID); chzzkChannelID != "" {
		if err := as.cache.HSet(ctx, ChzzkChannelMapKey, channelID, chzzkChannelID); err != nil {
			return fmt.Errorf("upsert chzzk mapping: %w", err)
		}
	} else if err := as.cache.HDel(ctx, ChzzkChannelMapKey, channelID); err != nil {
		return fmt.Errorf("delete missing chzzk mapping: %w", err)
	}

	twitchLogin := stringutil.Normalize(member.TwitchUserID)
	if err := as.reconcileTwitchMappingsForChannel(ctx, channelID, twitchLogin); err != nil {
		return fmt.Errorf("reconcile twitch mapping: %w", err)
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

	if len(mappings) == 0 {
		return nil
	}

	fields := make(map[string]any, len(mappings))
	for field, value := range mappings {
		fields[field] = value
	}

	if err := as.cache.HMSet(ctx, key, fields); err != nil {
		return fmt.Errorf("hmset key %s: %w", key, err)
	}

	return nil
}

func (as *AlarmService) removePlatformMappingsForChannel(ctx context.Context, channelID string) error {
	if err := as.cache.HDel(ctx, ChzzkChannelMapKey, channelID); err != nil {
		return fmt.Errorf("delete chzzk mapping: %w", err)
	}

	if err := as.reconcileTwitchMappingsForChannel(ctx, channelID, ""); err != nil {
		return fmt.Errorf("delete twitch mapping: %w", err)
	}

	return nil
}

func (as *AlarmService) reconcileTwitchMappingsForChannel(ctx context.Context, channelID, desiredLogin string) error {
	currentLogin, err := as.cache.HGet(ctx, TwitchChannelLoginMapKey, channelID)
	if err != nil {
		return fmt.Errorf("get current twitch channel login: %w", err)
	}

	if currentLogin != "" && currentLogin != desiredLogin {
		if delErr := as.cache.HDel(ctx, TwitchLoginMapKey, currentLogin); delErr != nil {
			return fmt.Errorf("delete stale twitch login mapping: %w", delErr)
		}
	}

	if desiredLogin == "" {
		if delErr := as.cache.HDel(ctx, TwitchChannelLoginMapKey, channelID); delErr != nil {
			return fmt.Errorf("delete twitch channel login mapping: %w", delErr)
		}

		return nil
	}

	existingChannelID, err := as.cache.HGet(ctx, TwitchLoginMapKey, desiredLogin)
	if err != nil {
		return fmt.Errorf("get desired twitch login mapping: %w", err)
	}

	if existingChannelID != "" && existingChannelID != channelID {
		if err := as.cache.HDel(ctx, TwitchChannelLoginMapKey, channelID); err != nil {
			return fmt.Errorf("clear conflicting twitch channel login mapping: %w", err)
		}

		if as.logger != nil {
			as.logger.Warn("Duplicate Twitch login detected while incrementally syncing platform mappings",
				slog.String("twitch_login", desiredLogin),
				slog.String("kept_channel_id", existingChannelID),
				slog.String("ignored_channel_id", channelID),
			)
		}

		return nil
	}

	if err := as.cache.HSet(ctx, TwitchLoginMapKey, desiredLogin, channelID); err != nil {
		return fmt.Errorf("upsert twitch mapping: %w", err)
	}

	if err := as.cache.HSet(ctx, TwitchChannelLoginMapKey, channelID, desiredLogin); err != nil {
		return fmt.Errorf("upsert twitch channel login mapping: %w", err)
	}

	return nil
}
