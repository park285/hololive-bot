package acl

import (
	"context"
	"strings"
	"testing"

	sharedtestutil "github.com/kapu/hololive-shared/pkg/testutil"
)

func TestRenameRoomsKeyMissingTempPreservesExistingRooms(t *testing.T) {
	ctx := context.Background()
	cacheClient := sharedtestutil.NewTestCacheService(t, ctx)
	service := &Service{cache: cacheClient}

	if _, err := cacheClient.SAdd(ctx, aclWhitelistRoomsKey, []string{"legacy-room"}); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	err := service.renameRoomsKey(ctx, "missing-temp", aclWhitelistRoomsKey, []string{"room-a"})
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
