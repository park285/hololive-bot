package collection

import (
	"fmt"
	"testing"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

func TestLiveSnapshotGenerationFollowsProviderContract(t *testing.T) {
	t.Parallel()

	jobs := map[contract.Provider]string{
		contract.ProviderYouTubeJS:        "youtubejs_channel_live",
		contract.ProviderHolodex:          "holodex_live",
		contract.ProviderHololiveOfficial: "official_schedule",
	}

	for provider, kind := range jobs {
		for _, generation := range []int64{1, contract.LiveSnapshotMetadataContractGeneration, contract.LiveSnapshotQueryContractGeneration, 4} {
			t.Run(fmt.Sprintf("%s/generation-%d", provider, generation), func(t *testing.T) {
				t.Parallel()

				snapshot, err := NewContractSnapshot([]contract.ObservationKind{contract.KindLiveSnapshot}, map[contract.ObservationKind]int64{
					contract.KindLiveSnapshot: generation,
				})
				if err != nil {
					t.Fatal(err)
				}

				input := RunInput{job: mustLookupJob(t, provider, kind), contracts: snapshot}

				err = input.RequireLiveSnapshotMetadataGeneration()

				accepted := provider == contract.ProviderYouTubeJS && generation == contract.LiveSnapshotQueryContractGeneration ||
					provider == contract.ProviderHolodex && generation == contract.LiveSnapshotMetadataContractGeneration

				if accepted {
					if err != nil {
						t.Fatalf("current provider contract rejected: %v", err)
					}

					return
				}

				if err == nil || collecterr.CodeOf(err) != collecterr.Configuration {
					t.Fatalf("unsupported provider generation accepted: %v", err)
				}
			})
		}
	}
}
