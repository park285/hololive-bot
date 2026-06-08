package acl

import (
	"strings"
	"testing"
)

func TestRenameRoomsKeyScriptDeclaresTouchedKeysAsKeys(t *testing.T) {
	for _, want := range []string{"local source = KEYS[1]", "local target = KEYS[2]"} {
		if !strings.Contains(renameRoomsKeyScript, want) {
			t.Fatalf("renameRoomsKeyScript missing %q", want)
		}
	}
	for _, forbidden := range []string{"local source = ARGV[1]", "local target = ARGV[2]"} {
		if strings.Contains(renameRoomsKeyScript, forbidden) {
			t.Fatalf("renameRoomsKeyScript must not smuggle key names through ARGV: %s", renameRoomsKeyScript)
		}
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
