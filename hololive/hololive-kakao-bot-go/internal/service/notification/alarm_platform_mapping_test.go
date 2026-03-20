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
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestSyncPlatformMappings_WritesChzzkAndTwitchHashes(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	as.memberData = &mockMemberDataProvider{
		members: []*domain.Member{
			{
				ChannelID:      "UC_alpha",
				ChzzkChannelID: "chzzk_alpha",
				TwitchUserID:   "AlphaLogin",
			},
			{
				ChannelID:      "UC_beta",
				ChzzkChannelID: "chzzk_beta",
			},
		},
	}

	if _, err := as.cache.SAdd(ctx, AlarmChannelRegistryKey, []string{"UC_alpha", "UC_beta", "UC_missing"}); err != nil {
		t.Fatalf("SAdd registry failed: %v", err)
	}

	if err := as.SyncPlatformMappings(ctx); err != nil {
		t.Fatalf("SyncPlatformMappings failed: %v", err)
	}

	chzzkMap, err := as.cache.HGetAll(ctx, ChzzkChannelMapKey)
	if err != nil {
		t.Fatalf("HGetAll chzzk map failed: %v", err)
	}

	if got, ok := chzzkMap["UC_alpha"]; !ok || got != "chzzk_alpha" {
		t.Fatalf("unexpected chzzk mapping for UC_alpha: %q", got)
	}

	if got, ok := chzzkMap["UC_beta"]; !ok || got != "chzzk_beta" {
		t.Fatalf("unexpected chzzk mapping for UC_beta: %q", got)
	}

	if _, exists := chzzkMap["UC_missing"]; exists {
		t.Fatal("unexpected chzzk mapping for UC_missing")
	}

	twitchMap, err := as.cache.HGetAll(ctx, TwitchLoginMapKey)
	if err != nil {
		t.Fatalf("HGetAll twitch map failed: %v", err)
	}

	if got, ok := twitchMap["alphalogin"]; !ok || got != "UC_alpha" {
		t.Fatalf("unexpected twitch mapping for alphalogin: %q", got)
	}

	if len(twitchMap) != 1 {
		t.Fatalf("unexpected twitch mapping size: got=%d map=%v", len(twitchMap), twitchMap)
	}

	twitchChannelMap, err := as.cache.HGetAll(ctx, TwitchChannelLoginMapKey)
	if err != nil {
		t.Fatalf("HGetAll twitch channel map failed: %v", err)
	}

	if got, ok := twitchChannelMap["UC_alpha"]; !ok || got != "alphalogin" {
		t.Fatalf("unexpected twitch channel mapping for UC_alpha: %q", got)
	}

	if len(twitchChannelMap) != 1 {
		t.Fatalf("unexpected twitch channel mapping size: got=%d map=%v", len(twitchChannelMap), twitchChannelMap)
	}
}

func TestSyncPlatformMappings_ClearsStaleHashes(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	as.memberData = &mockMemberDataProvider{members: []*domain.Member{}}

	if err := as.cache.HSet(ctx, ChzzkChannelMapKey, "UC_stale", "chzzk_stale"); err != nil {
		t.Fatalf("seed chzzk map failed: %v", err)
	}

	if err := as.cache.HSet(ctx, TwitchLoginMapKey, "stale_login", "UC_stale"); err != nil {
		t.Fatalf("seed twitch map failed: %v", err)
	}

	if err := as.SyncPlatformMappings(ctx); err != nil {
		t.Fatalf("SyncPlatformMappings failed: %v", err)
	}

	chzzkMap, err := as.cache.HGetAll(ctx, ChzzkChannelMapKey)
	if err != nil {
		t.Fatalf("HGetAll chzzk map failed: %v", err)
	}

	if len(chzzkMap) != 0 {
		t.Fatalf("expected empty chzzk map, got: %v", chzzkMap)
	}

	twitchMap, err := as.cache.HGetAll(ctx, TwitchLoginMapKey)
	if err != nil {
		t.Fatalf("HGetAll twitch map failed: %v", err)
	}

	if len(twitchMap) != 0 {
		t.Fatalf("expected empty twitch map, got: %v", twitchMap)
	}

	twitchChannelMap, err := as.cache.HGetAll(ctx, TwitchChannelLoginMapKey)
	if err != nil {
		t.Fatalf("HGetAll twitch channel map failed: %v", err)
	}

	if len(twitchChannelMap) != 0 {
		t.Fatalf("expected empty twitch channel map, got: %v", twitchChannelMap)
	}
}

func TestSyncPlatformMappingForChannel_AddAndRemoveIncrementally(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	as.memberData = &mockMemberDataProvider{
		members: []*domain.Member{
			{
				ChannelID:      "UC_alpha",
				ChzzkChannelID: "chzzk_alpha",
				TwitchUserID:   "AlphaLogin",
			},
		},
	}

	if _, err := as.cache.SAdd(ctx, AlarmChannelRegistryKey, []string{"UC_alpha"}); err != nil {
		t.Fatalf("SAdd registry failed: %v", err)
	}

	if err := as.syncPlatformMappingForChannel(ctx, "UC_alpha"); err != nil {
		t.Fatalf("syncPlatformMappingForChannel add failed: %v", err)
	}

	chzzkMap, err := as.cache.HGetAll(ctx, ChzzkChannelMapKey)
	if err != nil {
		t.Fatalf("HGetAll chzzk map failed: %v", err)
	}

	if got := chzzkMap["UC_alpha"]; got != "chzzk_alpha" {
		t.Fatalf("unexpected chzzk mapping: %q", got)
	}

	twitchMap, err := as.cache.HGetAll(ctx, TwitchLoginMapKey)
	if err != nil {
		t.Fatalf("HGetAll twitch map failed: %v", err)
	}

	if got := twitchMap["alphalogin"]; got != "UC_alpha" {
		t.Fatalf("unexpected twitch mapping: %q", got)
	}

	twitchChannelMap, err := as.cache.HGetAll(ctx, TwitchChannelLoginMapKey)
	if err != nil {
		t.Fatalf("HGetAll twitch channel map failed: %v", err)
	}

	if got := twitchChannelMap["UC_alpha"]; got != "alphalogin" {
		t.Fatalf("unexpected twitch channel mapping: %q", got)
	}

	if _, err := as.cache.SRem(ctx, AlarmChannelRegistryKey, []string{"UC_alpha"}); err != nil {
		t.Fatalf("SRem registry failed: %v", err)
	}

	if err := as.syncPlatformMappingForChannel(ctx, "UC_alpha"); err != nil {
		t.Fatalf("syncPlatformMappingForChannel remove failed: %v", err)
	}

	chzzkMap, err = as.cache.HGetAll(ctx, ChzzkChannelMapKey)
	if err != nil {
		t.Fatalf("HGetAll chzzk map failed: %v", err)
	}

	if len(chzzkMap) != 0 {
		t.Fatalf("expected empty chzzk map after remove, got: %v", chzzkMap)
	}

	twitchMap, err = as.cache.HGetAll(ctx, TwitchLoginMapKey)
	if err != nil {
		t.Fatalf("HGetAll twitch map failed: %v", err)
	}

	if len(twitchMap) != 0 {
		t.Fatalf("expected empty twitch map after remove, got: %v", twitchMap)
	}

	twitchChannelMap, err = as.cache.HGetAll(ctx, TwitchChannelLoginMapKey)
	if err != nil {
		t.Fatalf("HGetAll twitch channel map failed: %v", err)
	}

	if len(twitchChannelMap) != 0 {
		t.Fatalf("expected empty twitch channel map after remove, got: %v", twitchChannelMap)
	}
}

func TestSyncPlatformMappingForChannel_ReplacesTwitchLoginInO1Path(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	as.memberData = &mockMemberDataProvider{
		members: []*domain.Member{
			{
				ChannelID:    "UC_alpha",
				TwitchUserID: "NewLogin",
			},
		},
	}

	if _, err := as.cache.SAdd(ctx, AlarmChannelRegistryKey, []string{"UC_alpha"}); err != nil {
		t.Fatalf("SAdd registry failed: %v", err)
	}

	if err := as.cache.HSet(ctx, TwitchLoginMapKey, "oldlogin", "UC_alpha"); err != nil {
		t.Fatalf("seed twitch map failed: %v", err)
	}

	if err := as.cache.HSet(ctx, TwitchChannelLoginMapKey, "UC_alpha", "oldlogin"); err != nil {
		t.Fatalf("seed twitch channel map failed: %v", err)
	}

	if err := as.syncPlatformMappingForChannel(ctx, "UC_alpha"); err != nil {
		t.Fatalf("syncPlatformMappingForChannel replace failed: %v", err)
	}

	twitchMap, err := as.cache.HGetAll(ctx, TwitchLoginMapKey)
	if err != nil {
		t.Fatalf("HGetAll twitch map failed: %v", err)
	}

	if _, exists := twitchMap["oldlogin"]; exists {
		t.Fatalf("expected oldlogin to be removed, got: %v", twitchMap)
	}

	if got := twitchMap["newlogin"]; got != "UC_alpha" {
		t.Fatalf("unexpected new twitch mapping: %q", got)
	}

	twitchChannelMap, err := as.cache.HGetAll(ctx, TwitchChannelLoginMapKey)
	if err != nil {
		t.Fatalf("HGetAll twitch channel map failed: %v", err)
	}

	if got := twitchChannelMap["UC_alpha"]; got != "newlogin" {
		t.Fatalf("unexpected new twitch channel mapping: %q", got)
	}
}
