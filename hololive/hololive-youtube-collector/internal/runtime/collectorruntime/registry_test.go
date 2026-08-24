package collectorruntime

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
)

func TestNewRegistryRejectsDuplicateJob(t *testing.T) {
	t.Parallel()

	_, err := NewRegistry(
		stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindCommunityPage),
		stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindCommunityPage),
	)
	if err == nil {
		t.Fatal("duplicate runner must fail closed")
	}
}

func TestNewRegistryRejectsUnknownJob(t *testing.T) {
	t.Parallel()

	_, err := NewRegistry(stubJob(contract.ProviderYouTubeJS, "unknown_job", contract.KindVideoList))
	if err == nil {
		t.Fatal("unknown job must fail closed")
	}
}

func TestNewRegistryRequiresInitialJobCoverage(t *testing.T) {
	t.Parallel()

	_, err := NewRegistry(stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindCommunityPage))
	if err == nil {
		t.Fatal("incomplete InitialJobContracts coverage must fail closed")
	}
}

func TestNewRegistryAcceptsCompleteAdapterSet(t *testing.T) {
	t.Parallel()

	registry, err := NewRegistry(completeStubRunners()...)
	if err != nil {
		t.Fatal(err)
	}

	if len(registry.Runners()) != 9 {
		t.Fatalf("runners = %d", len(registry.Runners()))
	}
}

func TestExecutionProfileMinimumIncludesReservations(t *testing.T) {
	t.Parallel()

	profile, err := NewExecutionProfile(2, 3*time.Second, time.Second, 4, 2*time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}

	if got, want := profile.MinimumCollectTimeout(), 15*time.Second; got != want {
		t.Fatalf("minimum collect timeout = %s, want %s", got, want)
	}

	if profile.CollectTimeout() != profile.MinimumCollectTimeout() {
		t.Fatal("zero configured timeout did not select exact minimum")
	}
}

func TestExecutionProfileRejectsDurationOverflowAndUndersizedTimeout(t *testing.T) {
	t.Parallel()

	if _, err := NewExecutionProfile(math.MaxInt, time.Duration(math.MaxInt64), 0, 2, time.Second, 0); err == nil {
		t.Fatal("overflowing execution profile was accepted")
	}

	if _, err := NewExecutionProfile(2, time.Second, time.Second, 2, time.Second, 2*time.Second); err == nil {
		t.Fatal("undersized collect timeout was accepted")
	}
}

func completeStubRunners() []JobRunner {
	return []JobRunner{
		stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindCommunityPage),
		stubJob(contract.ProviderYouTubeJS, "youtubejs_content", contract.KindVideoList, contract.KindShortsList),
		stubJob(contract.ProviderYouTubeJS, "youtubejs_channel_live", contract.KindLiveSnapshot),
		stubJob(contract.ProviderYouTubeJS, "youtubejs_channel_metadata",
			contract.KindChannelStats, contract.KindChannelProfile, contract.KindChannelPhoto),
		stubJob(contract.ProviderYouTubeJS, "youtubejs_viewer", contract.KindViewerSample),
		stubJob(contract.ProviderHolodex, "holodex_live", contract.KindLiveSnapshot, contract.KindViewerSample),
		stubJob(contract.ProviderHolodex, "holodex_metadata", contract.KindChannelStats, contract.KindChannelPhoto),
		stubJob(contract.ProviderHolodex, "holodex_schedule", contract.KindSchedule),
		stubJob(contract.ProviderHololiveOfficial, "official_schedule", contract.KindSchedule),
	}
}

type stubRunner struct {
	provider contract.Provider
	jobKind  string
	collect  func(context.Context, *collectutil.RunInput) (collectutil.CollectResult, error)
}

func stubJob(provider contract.Provider, jobKind string, _ ...contract.ObservationKind) *stubRunner {
	return &stubRunner{provider: provider, jobKind: jobKind}
}

func (s *stubRunner) JobID() sourceobservation.JobID {
	return sourceobservation.JobID{Provider: s.provider, Kind: sourceobservation.JobKind(s.jobKind)}
}

func (s *stubRunner) Collect(ctx context.Context, input *collectutil.RunInput) (collectutil.CollectResult, error) {
	if s.collect != nil {
		out, err := s.collect(ctx, input)
		if err != nil {
			return out, fmt.Errorf("collect: %w", err)
		}

		return out, nil
	}

	out, err := collectutil.NewCompleteResult(collectutil.RunOutput{})
	if err != nil {
		return out, fmt.Errorf("complete result: %w", err)
	}

	return out, nil
}
