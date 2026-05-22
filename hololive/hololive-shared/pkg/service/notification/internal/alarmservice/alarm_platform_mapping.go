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

package alarmservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
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

	chzzkMappings, twitchMappings, twitchChannelMappings := as.collectPlatformMappings(channelIDs)

	if err := as.replaceHashMappingsWithEmptyMarker(ctx, ChzzkChannelMapKey, ChzzkChannelMapEmptyKey, chzzkMappings); err != nil {
		return fmt.Errorf("sync chzzk channel mappings: %w", err)
	}

	if err := as.replaceHashMappingsWithEmptyMarker(ctx, TwitchLoginMapKey, TwitchLoginMapEmptyKey, twitchMappings); err != nil {
		return fmt.Errorf("sync twitch login mappings: %w", err)
	}

	if err := as.replaceHashMappingsWithEmptyMarker(ctx, TwitchChannelLoginMapKey, TwitchChannelLoginMapEmptyKey, twitchChannelMappings); err != nil {
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

func (as *AlarmService) collectPlatformMappings(channelIDs []string) (map[string]string, map[string]string, map[string]string) {
	chzzkMappings := make(map[string]string, len(channelIDs))
	twitchMappings := make(map[string]string, len(channelIDs))
	twitchChannelMappings := make(map[string]string, len(channelIDs))

	for _, channelID := range channelIDs {
		as.collectPlatformMappingForChannel(channelID, chzzkMappings, twitchMappings, twitchChannelMappings)
	}

	return chzzkMappings, twitchMappings, twitchChannelMappings
}

func (as *AlarmService) collectPlatformMappingForChannel(
	channelID string,
	chzzkMappings map[string]string,
	twitchMappings map[string]string,
	twitchChannelMappings map[string]string,
) {
	member := as.memberData.FindMemberByChannelID(channelID)
	if member == nil {
		as.logUnknownPlatformMappingChannel(channelID)
		return
	}

	if chzzkChannelID := stringutil.TrimSpace(member.ChzzkChannelID); chzzkChannelID != "" {
		chzzkMappings[channelID] = chzzkChannelID
	}

	twitchLogin := stringutil.Normalize(member.TwitchUserID)
	if twitchLogin == "" {
		return
	}

	if existingChannelID, exists := twitchMappings[twitchLogin]; exists && existingChannelID != channelID {
		as.logDuplicateTwitchMapping(twitchLogin, existingChannelID, channelID)
		return
	}

	twitchMappings[twitchLogin] = channelID
	twitchChannelMappings[channelID] = twitchLogin
}

func (as *AlarmService) logUnknownPlatformMappingChannel(channelID string) {
	if as.logger == nil {
		return
	}

	as.logger.Warn("Skip platform mapping sync for unknown channel",
		slog.String("channel_id", channelID),
	)
}

func (as *AlarmService) logDuplicateTwitchMapping(twitchLogin, keptChannelID, ignoredChannelID string) {
	if as.logger == nil {
		return
	}

	as.logger.Warn("Duplicate Twitch login detected while syncing platform mappings",
		slog.String("twitch_login", twitchLogin),
		slog.String("kept_channel_id", keptChannelID),
		slog.String("ignored_channel_id", ignoredChannelID),
	)
}

func (as *AlarmService) syncPlatformMappingForChannel(ctx context.Context, channelID string) error {
	channelID = stringutil.TrimSpace(channelID)
	if channelID == "" {
		return nil
	}

	if err := as.validatePlatformMappingDependencies(); err != nil {
		return err
	}

	as.platformMapMu.Lock()
	defer as.platformMapMu.Unlock()

	registered, err := as.cache.SIsMember(ctx, AlarmChannelRegistryKey, channelID)
	if err != nil {
		return fmt.Errorf("check channel registry membership: %w", err)
	}

	if !registered {
		return as.removeStalePlatformMappingsForChannel(ctx, channelID)
	}

	return as.syncRegisteredPlatformMappingForChannel(ctx, channelID)
}

func (as *AlarmService) validatePlatformMappingDependencies() error {
	if as.cache == nil {
		return errors.New("cache service not configured")
	}
	if as.memberData == nil {
		return errors.New("member data provider not configured")
	}
	return nil
}

func (as *AlarmService) removeStalePlatformMappingsForChannel(ctx context.Context, channelID string) error {
	if err := as.removePlatformMappingsForChannel(ctx, channelID); err != nil {
		return fmt.Errorf("remove stale platform mappings: %w", err)
	}
	return nil
}

func (as *AlarmService) syncRegisteredPlatformMappingForChannel(ctx context.Context, channelID string) error {
	member := as.memberData.FindMemberByChannelID(channelID)
	if member == nil {
		return as.removeUnknownPlatformMappingsForChannel(ctx, channelID)
	}

	if err := as.syncChzzkMappingForChannel(ctx, channelID, member.ChzzkChannelID); err != nil {
		return err
	}

	twitchLogin := stringutil.Normalize(member.TwitchUserID)
	if err := as.reconcileTwitchMappingsForChannel(ctx, channelID, twitchLogin); err != nil {
		return fmt.Errorf("reconcile twitch mapping: %w", err)
	}

	return nil
}

func (as *AlarmService) removeUnknownPlatformMappingsForChannel(ctx context.Context, channelID string) error {
	logging.Warn(ctx, as.logger, "platform_mapping.unknown_channel", "Skip platform mapping update for unknown channel",
		slog.String("channel_id", channelID),
	)

	if err := as.removePlatformMappingsForChannel(ctx, channelID); err != nil {
		return fmt.Errorf("remove unknown channel platform mappings: %w", err)
	}
	return nil
}

func (as *AlarmService) syncChzzkMappingForChannel(ctx context.Context, channelID, rawChzzkChannelID string) error {
	chzzkChannelID := stringutil.TrimSpace(rawChzzkChannelID)
	if chzzkChannelID == "" {
		if err := as.cache.HDel(ctx, ChzzkChannelMapKey, channelID); err != nil {
			return fmt.Errorf("delete missing chzzk mapping: %w", err)
		}
		return nil
	}

	if err := as.cache.HSet(ctx, ChzzkChannelMapKey, channelID, chzzkChannelID); err != nil {
		return fmt.Errorf("upsert chzzk mapping: %w", err)
	}
	if err := as.cache.Del(ctx, ChzzkChannelMapEmptyKey); err != nil {
		return fmt.Errorf("clear chzzk empty marker: %w", err)
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

func (as *AlarmService) removeStaleTwitchLoginMappingIfOwned(ctx context.Context, login, channelID string) error {
	login = stringutil.Normalize(login)
	channelID = stringutil.TrimSpace(channelID)
	if login == "" || channelID == "" {
		return nil
	}

	owner, err := as.cache.HGet(ctx, TwitchLoginMapKey, login)
	if err != nil {
		return fmt.Errorf("get stale twitch login owner: %w", err)
	}
	if owner != "" && owner != channelID {
		return nil
	}

	if err := as.cache.HDel(ctx, TwitchLoginMapKey, login); err != nil {
		return fmt.Errorf("delete stale twitch login mapping: %w", err)
	}

	return nil
}

func (as *AlarmService) reconcileTwitchMappingsForChannel(ctx context.Context, channelID, desiredLogin string) error {
	channelID = stringutil.TrimSpace(channelID)
	desiredLogin = stringutil.Normalize(desiredLogin)

	currentLogin, err := as.currentTwitchChannelLogin(ctx, channelID)
	if err != nil {
		return err
	}

	if err := as.removeChangedTwitchLoginMapping(ctx, currentLogin, channelID, desiredLogin); err != nil {
		return err
	}

	if desiredLogin == "" {
		return nil
	}

	return as.reconcileDesiredTwitchLoginMapping(ctx, channelID, desiredLogin)
}

func (as *AlarmService) reconcileDesiredTwitchLoginMapping(ctx context.Context, channelID, desiredLogin string) error {
	existingChannelID, err := as.cache.HGet(ctx, TwitchLoginMapKey, desiredLogin)
	if err != nil {
		return fmt.Errorf("get desired twitch login mapping: %w", err)
	}

	if existingChannelID != "" && existingChannelID != channelID {
		return as.clearConflictingTwitchChannelLoginMapping(ctx, desiredLogin, existingChannelID, channelID)
	}

	return as.upsertTwitchMappingsForChannel(ctx, channelID, desiredLogin)
}

func (as *AlarmService) currentTwitchChannelLogin(ctx context.Context, channelID string) (string, error) {
	currentLogin, err := as.cache.HGet(ctx, TwitchChannelLoginMapKey, channelID)
	if err != nil {
		return "", fmt.Errorf("get current twitch channel login: %w", err)
	}
	return stringutil.Normalize(currentLogin), nil
}

func (as *AlarmService) removeChangedTwitchLoginMapping(
	ctx context.Context,
	currentLogin string,
	channelID string,
	desiredLogin string,
) error {
	if currentLogin != "" && currentLogin != desiredLogin {
		if err := as.removeStaleTwitchLoginMappingIfOwned(ctx, currentLogin, channelID); err != nil {
			return fmt.Errorf("delete stale twitch login mapping: %w", err)
		}
	}
	if desiredLogin == "" {
		return as.deleteTwitchChannelLoginMapping(ctx, channelID)
	}
	return nil
}

func (as *AlarmService) deleteTwitchChannelLoginMapping(ctx context.Context, channelID string) error {
	if err := as.cache.HDel(ctx, TwitchChannelLoginMapKey, channelID); err != nil {
		return fmt.Errorf("delete twitch channel login mapping: %w", err)
	}
	return nil
}

func (as *AlarmService) clearConflictingTwitchChannelLoginMapping(
	ctx context.Context,
	desiredLogin string,
	existingChannelID string,
	channelID string,
) error {
	if err := as.cache.HDel(ctx, TwitchChannelLoginMapKey, channelID); err != nil {
		return fmt.Errorf("clear conflicting twitch channel login mapping: %w", err)
	}

	logging.Warn(ctx, as.logger, "platform_mapping.duplicate_twitch_login", "Duplicate Twitch login detected while incrementally syncing platform mappings",
		slog.String("twitch_login", desiredLogin),
		slog.String("kept_channel_id", existingChannelID),
		slog.String("ignored_channel_id", channelID),
	)
	return nil
}

func (as *AlarmService) upsertTwitchMappingsForChannel(ctx context.Context, channelID, desiredLogin string) error {
	if err := as.cache.HSet(ctx, TwitchLoginMapKey, desiredLogin, channelID); err != nil {
		return fmt.Errorf("upsert twitch mapping: %w", err)
	}
	if err := as.cache.Del(ctx, TwitchLoginMapEmptyKey); err != nil {
		return fmt.Errorf("clear twitch empty marker: %w", err)
	}

	if err := as.cache.HSet(ctx, TwitchChannelLoginMapKey, channelID, desiredLogin); err != nil {
		return fmt.Errorf("upsert twitch channel login mapping: %w", err)
	}
	if err := as.cache.Del(ctx, TwitchChannelLoginMapEmptyKey); err != nil {
		return fmt.Errorf("clear twitch channel empty marker: %w", err)
	}

	return nil
}
