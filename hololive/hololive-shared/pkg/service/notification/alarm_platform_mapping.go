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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
	"github.com/valkey-io/valkey-go"
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
		member := as.memberData.FindMemberByChannelID(channelID)
		if member == nil {
			as.logUnknownPlatformMappingChannel(channelID)

			continue
		}

		if chzzkChannelID := stringutil.TrimSpace(member.ChzzkChannelID); chzzkChannelID != "" {
			chzzkMappings[channelID] = chzzkChannelID
		}

		twitchLogin := stringutil.Normalize(member.TwitchUserID)
		if twitchLogin == "" {
			continue
		}

		if existingChannelID, exists := twitchMappings[twitchLogin]; exists && existingChannelID != channelID {
			as.logDuplicateTwitchMapping(twitchLogin, existingChannelID, channelID)

			continue
		}

		twitchMappings[twitchLogin] = channelID
		twitchChannelMappings[channelID] = twitchLogin
	}

	return chzzkMappings, twitchMappings, twitchChannelMappings
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
		if err := as.cache.Del(ctx, ChzzkChannelMapEmptyKey); err != nil {
			return fmt.Errorf("clear chzzk empty marker: %w", err)
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

const replaceHashMappingsScript = `
local source = ARGV[1]
local target = ARGV[2]
if redis.call('EXISTS', source) == 1 then
  redis.call('RENAME', source, target)
else
  redis.call('DEL', target)
end
return 1
`

func (as *AlarmService) replaceHashMappings(
	ctx context.Context,
	key string,
	mappings map[string]string,
) error {
	key = stringutil.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("mapping key is empty")
	}

	fields := make(map[string]any, len(mappings))
	for field, value := range mappings {
		field = stringutil.TrimSpace(field)
		value = stringutil.TrimSpace(value)
		if field == "" || value == "" {
			continue
		}
		fields[field] = value
	}

	if len(fields) == 0 {
		if err := as.cache.Del(ctx, key); err != nil {
			return fmt.Errorf("delete empty mapping key %s: %w", key, err)
		}

		return nil
	}

	tmpKey := fmt.Sprintf("%s:tmp:%d", key, time.Now().UnixNano())
	if err := as.cache.Del(ctx, tmpKey); err != nil {
		return fmt.Errorf("delete temp mapping key %s: %w", tmpKey, err)
	}

	if err := as.cache.HMSet(ctx, tmpKey, fields); err != nil {
		_ = as.cache.Del(context.WithoutCancel(ctx), tmpKey)
		return fmt.Errorf("hmset temp mapping key %s: %w", tmpKey, err)
	}

	if err := as.renameHashMappingKey(ctx, tmpKey, key, fields); err != nil {
		_ = as.cache.Del(context.WithoutCancel(ctx), tmpKey)
		return fmt.Errorf("rename mapping key %s from %s: %w", key, tmpKey, err)
	}

	return nil
}

func (as *AlarmService) replaceHashMappingsWithEmptyMarker(
	ctx context.Context,
	key string,
	emptyMarkerKey string,
	mappings map[string]string,
) error {
	if err := as.replaceHashMappings(ctx, key, mappings); err != nil {
		return err
	}

	if len(mappings) == 0 {
		if err := as.cache.Set(ctx, emptyMarkerKey, "1", 0); err != nil {
			return fmt.Errorf("set empty marker %s: %w", emptyMarkerKey, err)
		}

		return nil
	}

	if err := as.cache.Del(ctx, emptyMarkerKey); err != nil {
		return fmt.Errorf("clear empty marker %s: %w", emptyMarkerKey, err)
	}

	return nil
}

func (as *AlarmService) renameHashMappingKey(ctx context.Context, tmpKey, key string, fields map[string]any) error {
	client, builder, ok := as.rawPlatformMappingEvalClient()
	if !ok {
		if err := as.cache.Del(ctx, key); err != nil {
			return fmt.Errorf("fallback delete key %s: %w", key, err)
		}
		if err := as.cache.HMSet(ctx, key, fields); err != nil {
			return fmt.Errorf("fallback hmset key %s: %w", key, err)
		}
		return nil
	}

	resp := client.Do(ctx, builder.Eval().Script(replaceHashMappingsScript).Numkeys(0).Arg(tmpKey, key).Build())
	if err := resp.Error(); err != nil {
		return fmt.Errorf("eval rename key %s from %s: %w", key, tmpKey, err)
	}

	return nil
}

func (as *AlarmService) rawPlatformMappingEvalClient() (_ valkey.Client, _ valkey.Builder, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	client := as.cache.GetClient()
	builder := as.cache.B()
	if client == nil {
		return nil, valkey.Builder{}, false
	}

	return client, builder, true
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

	currentLogin, err := as.cache.HGet(ctx, TwitchChannelLoginMapKey, channelID)
	if err != nil {
		return fmt.Errorf("get current twitch channel login: %w", err)
	}
	currentLogin = stringutil.Normalize(currentLogin)

	if currentLogin != "" && currentLogin != desiredLogin {
		if delErr := as.removeStaleTwitchLoginMappingIfOwned(ctx, currentLogin, channelID); delErr != nil {
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
