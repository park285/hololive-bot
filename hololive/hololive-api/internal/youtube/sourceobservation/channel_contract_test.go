package sourceobservation

import (
	"testing"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func TestChannelMetadataSupportedProviders(t *testing.T) {
	t.Parallel()

	supported := InitialSupportedContracts()

	for _, test := range []struct {
		provider contract.Provider
		kind     contract.ObservationKind
		want     bool
	}{
		{contract.ProviderHolodex, contract.KindChannelProfile, false},
		{contract.ProviderHolodex, contract.KindChannelPhoto, true},
		{contract.ProviderYouTubeJS, contract.KindChannelProfile, true},
		{contract.ProviderYouTubeJS, contract.KindChannelPhoto, true},
	} {
		if got := supported.Supports(ContractVersion{test.provider, test.kind, 1, 1}); got != test.want {
			t.Errorf("%s/%s supported=%t, want %t", test.provider, test.kind, got, test.want)
		}
	}
}
