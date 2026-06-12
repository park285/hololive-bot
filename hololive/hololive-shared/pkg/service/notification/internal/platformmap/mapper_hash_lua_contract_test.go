package platformmap

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

func TestRenameHashMappingMissingTempPreservesExistingHash(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	mapper := NewMapper(cacheClient, func() domain.MemberDataProvider {
		return &stubMemberDataProvider{}
	}, slog.Default())

	if err := cacheClient.HSet(ctx, TwitchLoginMapKey, "legacy-login", "UC_legacy"); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	err := mapper.renameHashMappingKey(ctx, "missing-temp", TwitchLoginMapKey, map[string]any{
		"new-login": "UC_new",
	})
	if err == nil {
		t.Fatal("expected missing temp rename to fail")
	}

	got, err := cacheClient.HGetAll(ctx, TwitchLoginMapKey)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	want := map[string]string{"legacy-login": "UC_legacy"}
	if len(got) != len(want) || got["legacy-login"] != want["legacy-login"] {
		t.Fatalf("target hash=%v want=%v", got, want)
	}
}

func TestPlatformMapTempKeyUsesTargetKeyAsClusterHashTagWhenNeeded(t *testing.T) {
	platformMapTempKeySeq.Store(0)
	tempKey := platformMapTempKey("alarm:chzzk_channels")
	if !strings.HasPrefix(tempKey, "{alarm:chzzk_channels}:tmp:") {
		t.Fatalf("temp key = %q, want target key wrapped as hash tag", tempKey)
	}
}

func TestPlatformMapTempKeyPreservesExistingHashTag(t *testing.T) {
	platformMapTempKeySeq.Store(0)
	tempKey := platformMapTempKey("alarm:{chzzk}:channels")
	if !strings.HasPrefix(tempKey, "alarm:{chzzk}:channels:tmp:") {
		t.Fatalf("temp key = %q, want existing hash tag preserved", tempKey)
	}
}
