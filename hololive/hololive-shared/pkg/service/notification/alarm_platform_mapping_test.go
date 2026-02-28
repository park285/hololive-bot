package notification

import (
	"context"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestSyncPlatformMappings_WritesChzzkAndTwitchHashes(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := context.Background()

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
		t.Fatalf("unexpected chzzk mapping for UC_missing")
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
}

func TestSyncPlatformMappings_ClearsStaleHashes(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := context.Background()
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
}
