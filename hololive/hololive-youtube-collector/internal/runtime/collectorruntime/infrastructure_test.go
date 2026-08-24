package collectorruntime

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
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

func TestInfrastructureUsesConfiguredProviderTimeouts(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("infrastructure.go")
	if err != nil {
		t.Fatalf("read infrastructure source: %v", err)
	}

	text := string(source)
	if strings.Contains(text, "providerRequestTimeout") {
		t.Fatal("collector must not fall back provider request timeouts")
	}

	if !strings.Contains(text, "appConfig.Holodex.Transport.Timeout") || !strings.Contains(text, "appConfig.OfficialSchedule.Transport.Timeout") {
		t.Fatal("provider transport must use appConfig timeouts")
	}

	for _, forbidden := range []string{"ProvideCacheResources", "cache.Client", "service/cache", "appConfig.Valkey"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("collector infrastructure must not use cache: found %q", forbidden)
		}
	}
}

func TestHTTP014TransportCapEqualsProviderGate(t *testing.T) {
	t.Parallel()

	collector := settings.DefaultYouTubeCollectorConfig()

	collector.HolodexMaxInflight = 3
	collector.OfficialMaxInflight = 2

	gates := newProviderGates(&collector)
	holodex := providerTransportConfig(25*time.Second, collector.HolodexMaxInflight)
	official := providerTransportConfig(15*time.Second, collector.OfficialMaxInflight)

	if holodex.MaxConnsPerHost != cap(gates[contract.ProviderHolodex]) {
		t.Fatalf("holodex cap %d != gate %d", holodex.MaxConnsPerHost, cap(gates[contract.ProviderHolodex]))
	}

	if official.MaxConnsPerHost != cap(gates[contract.ProviderHololiveOfficial]) {
		t.Fatalf("official cap %d != gate %d", official.MaxConnsPerHost, cap(gates[contract.ProviderHololiveOfficial]))
	}
}

func TestInfrastructureClosesOwnedProviderClientsBeforeHelper(t *testing.T) {
	t.Parallel()

	source, err := os.ReadFile("infrastructure.go")
	if err != nil {
		t.Fatalf("read infrastructure source: %v", err)
	}

	text := string(source)
	if !strings.Contains(text, "collector.HolodexMaxInflight") || !strings.Contains(text, "collector.OfficialMaxInflight") {
		t.Fatal("provider transport must use collector inflight")
	}

	officialIdx := strings.Index(text, "i.official.Close()")
	holodexIdx := strings.Index(text, "i.holodex.Close()")
	helperIdx := strings.Index(text, "i.youtubejs.Close(ctx)")

	if officialIdx < 0 || holodexIdx < 0 || helperIdx < 0 || officialIdx > helperIdx || holodexIdx > helperIdx {
		t.Fatal("owned provider Close must run before helper Close")
	}
}
