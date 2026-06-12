package platformmap

import (
	"strings"
	"testing"
)

func TestReplaceHashMappingsScriptDeclaresTouchedKeysAsKeys(t *testing.T) {
	for _, want := range []string{"local source = KEYS[1]", "local target = KEYS[2]"} {
		if !strings.Contains(replaceHashMappingsScript, want) {
			t.Fatalf("replaceHashMappingsScript missing %q", want)
		}
	}
	for _, forbidden := range []string{"local source = ARGV[1]", "local target = ARGV[2]"} {
		if strings.Contains(replaceHashMappingsScript, forbidden) {
			t.Fatalf("replaceHashMappingsScript must not smuggle key names through ARGV: %s", replaceHashMappingsScript)
		}
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
