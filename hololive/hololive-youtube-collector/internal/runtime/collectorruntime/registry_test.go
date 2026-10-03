package collectorruntime

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

func TestNewRegistryRejectsDuplicateJob(t *testing.T) {
	t.Parallel()

	_, err := newTestRegistry(
		stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindCommunityPage),
		stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindCommunityPage),
	)
	if err == nil {
		t.Fatal("duplicate runner must fail closed")
	}
}

func TestNewRegistryRejectsUnknownJob(t *testing.T) {
	t.Parallel()

	_, err := newTestRegistry(stubJob(contract.ProviderYouTubeJS, "unknown_job", contract.KindVideoList))
	if err == nil {
		t.Fatal("unknown job must fail closed")
	}
}

func TestNewRegistryRequiresInitialJobCoverage(t *testing.T) {
	t.Parallel()

	_, err := newTestRegistry(stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindCommunityPage))
	if err == nil {
		t.Fatal("incomplete InitialJobContracts coverage must fail closed")
	}
}

func TestNewRegistryAcceptsCompleteAdapterSet(t *testing.T) {
	t.Parallel()

	if _, err := newTestRegistry(completeStubRunners()...); err != nil {
		t.Fatal(err)
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

// newTestRegistry는 모든 runner에 1초 실행 프로필을 붙여 운영 registry 생성 경로를 그대로 거칩니다.
func newTestRegistry(runners ...collection.JobRunner) (*Registry, error) {
	profiles := make(map[collection.JobID]ExecutionProfile, len(runners))
	for _, runner := range runners {
		if runner == nil {
			continue
		}

		profile, err := NewExecutionProfile(jobMaxUpstreamCalls(runner.JobID()), time.Second, 0, 1, time.Second, 0)
		if err != nil {
			return nil, fmt.Errorf("execution profile: %w", err)
		}

		profiles[runner.JobID()] = profile
	}

	out, err := NewRegistryWithProfiles(profiles, runners...)
	if err != nil {
		return nil, fmt.Errorf("registry with profiles: %w", err)
	}

	return out, nil
}

func completeStubRunners() []collection.JobRunner {
	return []collection.JobRunner{
		stubJob(contract.ProviderYouTubeJS, "community_collect", contract.KindCommunityPage),
		stubJob(contract.ProviderYouTubeJS, "youtubejs_content", contract.KindVideoList, contract.KindShortsList),
		stubJob(contract.ProviderYouTubeJS, "youtubejs_channel_live", contract.KindLiveSnapshot),
		stubJob(contract.ProviderYouTubeJS, "youtubejs_channel_live_check", contract.KindChannelLiveCheck),
		stubJob(contract.ProviderYouTubeJS, "youtubejs_channel_metadata",
			contract.KindChannelProfile, contract.KindChannelPhoto),
		stubJob(contract.ProviderYouTubeJS, "youtubejs_video_live", contract.KindVideoLiveCheck),
		stubJob(contract.ProviderHolodex, "holodex_live", contract.KindLiveSnapshot),
		stubJob(contract.ProviderHolodex, "holodex_metadata", contract.KindChannelPhoto),
		stubJob(contract.ProviderHolodex, "holodex_schedule", contract.KindSchedule),
		stubJob(contract.ProviderHololiveOfficial, "official_schedule", contract.KindSchedule),
	}
}

type stubRunner struct {
	provider contract.Provider
	jobKind  string
	collect  func(context.Context, *collection.RunInput) (collection.CollectResult, error)
}

func stubJob(provider contract.Provider, jobKind string, _ ...contract.ObservationKind) *stubRunner {
	return &stubRunner{provider: provider, jobKind: jobKind}
}

func (s *stubRunner) JobID() collection.JobID {
	return collection.JobID{Provider: s.provider, Kind: collection.JobKind(s.jobKind)}
}

func (s *stubRunner) Collect(ctx context.Context, input *collection.RunInput) (collection.CollectResult, error) {
	if s.collect != nil {
		out, err := s.collect(ctx, input)
		if err != nil {
			return out, fmt.Errorf("collect: %w", err)
		}

		return out, nil
	}

	return collection.NewCompleteResult(collection.RunOutput{}), nil
}
