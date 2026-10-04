package collectorruntime

import (
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	collectorconfig "github.com/kapu/hololive-youtube-collector/internal/config"
)

func TestHTTP014TransportCapEqualsProviderGate(t *testing.T) {
	t.Parallel()

	collector := collectorconfig.DefaultConfig()

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
