package collectorruntime

import (
	"os"
	"strings"
	"testing"
)

func TestCollectorInfrastructureAvoidsUnusedMemberCache(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("infrastructure.go")
	if err != nil {
		t.Fatalf("read infrastructure source: %v", err)
	}
	text := string(source)
	for _, forbidden := range []string{"BuildInfraModule", "MemberCache", "memberCache"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("collector infrastructure must not initialize unused member cache: found %q", forbidden)
		}
	}
}
