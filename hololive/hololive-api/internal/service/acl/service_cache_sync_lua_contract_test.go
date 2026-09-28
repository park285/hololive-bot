package acl

import (
	"context"
	"strings"
	"testing"

	"github.com/valkey-io/valkey-go"

	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

func TestRenameRoomsKeyMissingTempPreservesExistingRooms(t *testing.T) {
	ctx := t.Context()
	cacheClient := sharedtestutil.NewTestCacheService(ctx, t)
	service := &Service{cache: cacheClient}

	if _, err := cacheClient.SAdd(ctx, aclWhitelistRoomsKey, []string{"legacy-room"}); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	err := service.renameRoomsKey(ctx, "missing-temp", aclWhitelistRoomsKey, []string{testRoomA})
	if err == nil {
		t.Fatal("expected missing temp rename to fail")
	}

	got, err := cacheClient.SMembers(ctx, aclWhitelistRoomsKey)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}

	if len(got) != 1 || got[0] != "legacy-room" {
		t.Fatalf("target rooms=%v want=[legacy-room]", got)
	}
}

func TestACLRoomsTempKeyUsesTargetKeyAsClusterHashTagWhenNeeded(t *testing.T) {
	aclRoomsTempKeySeq.Store(0)

	tempKey := aclRoomsTempKey("acl:rooms")
	if !strings.HasPrefix(tempKey, "{acl:rooms}:tmp:") {
		t.Fatalf("temp key = %q, want target key wrapped as hash tag", tempKey)
	}
}

func TestACLRoomsTempKeyPreservesExistingHashTag(t *testing.T) {
	aclRoomsTempKeySeq.Store(0)

	tempKey := aclRoomsTempKey("acl:{rooms}")
	if !strings.HasPrefix(tempKey, "acl:{rooms}:tmp:") {
		t.Fatalf("temp key = %q, want existing hash tag preserved", tempKey)
	}
}

// raw client가 없는 형상에서 target key를 Del→SAdd로 바꾸는 비원자 경로는 없다. 조회자가 빈 집합을 보는 창을
// 만들지 않도록 오류를 돌려주고 기존 target을 그대로 둔다.
func TestSyncRoomsToValkeyWithoutRawClientFailsWithoutTouchingTarget(t *testing.T) {
	ctx := t.Context()

	var targetWrites []string

	cacheClient := &cachemocks.Client{
		DelFunc: func(_ context.Context, key string) error {
			if key == aclWhitelistRoomsKey {
				targetWrites = append(targetWrites, "del")
			}

			return nil
		},
		SAddFunc: func(_ context.Context, key string, members []string) (int64, error) {
			if key == aclWhitelistRoomsKey {
				targetWrites = append(targetWrites, "sadd")
			}

			return int64(len(members)), nil
		},
		GetClientFunc: func() valkey.Client { return nil },
		BFunc:         func() valkey.Builder { return valkey.Builder{} },
	}
	service := &Service{cache: cacheClient}

	if err := service.syncRoomsToValkeyAtomic(ctx, aclWhitelistRoomsKey, []string{testRoomA}); err == nil {
		t.Fatal("sync without raw valkey client must fail")
	}

	if len(targetWrites) != 0 {
		t.Fatalf("target key writes = %v, want none", targetWrites)
	}
}
