package youtubejscollector

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-collector/internal/runtime/collection"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const ChannelLiveJobKind collection.JobKind = "youtubejs_channel_" + channelKindLive

type ChannelLiveRunner struct {
	client ChannelClient
}

func NewChannelLiveRunner(client ChannelClient) *ChannelLiveRunner {
	return &ChannelLiveRunner{client: client}
}

func (r *ChannelLiveRunner) JobID() collection.JobID {
	return collection.JobID{Provider: contract.ProviderYouTubeJS, Kind: ChannelLiveJobKind}
}

func (r *ChannelLiveRunner) Collect(ctx context.Context, input *collection.RunInput) (collection.CollectResult, error) {
	if r == nil || r.client == nil {
		return collection.CollectResult{}, collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "youtube.js channel client is not configured")
	}

	if input == nil {
		return collection.CollectResult{}, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "collection run input is nil")
	}

	started := time.Now()

	allowed, err := input.Allows(contract.KindLiveSnapshot, input.Subject())
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("allows: %w", err)
	}

	if !allowed {
		return completeEmptyCollection(started)
	}

	snapshot, err := r.liveSnapshotEnvelope(ctx, input)
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("live snapshot: %w", err)
	}

	var envelopes []contract.Envelope

	if snapshot != nil {
		envelopes = []contract.Envelope{*snapshot}
	}

	out, err := collection.CompleteFromEnvelopes(envelopes, started)
	if err != nil {
		return out, fmt.Errorf("complete from envelopes: %w", err)
	}

	return out, nil
}

func (r *ChannelLiveRunner) liveSnapshotEnvelope(ctx context.Context, input *collection.RunInput) (*contract.Envelope, error) {
	result, completeness, continuity, err := fetchChannelPage(ctx, r.client, input, channelKindLive)
	if err != nil {
		return nil, fmt.Errorf("fetch channel page: %w", err)
	}

	if validateErr := validateLiveSchedules(result.LiveSessions, input.Lease().ScheduledFor); validateErr != nil {
		return nil, fmt.Errorf("validate live schedules: %w", validateErr)
	}

	if result.MissingTab {
		//nolint:nilnil // 방송 탭이 없으면 방출할 snapshot이 없다는 뜻이라 오류가 아니다.
		return nil, nil
	}

	if generationErr := input.RequireLiveSnapshotMetadataGeneration(); generationErr != nil {
		return nil, fmt.Errorf("require live snapshot metadata generation: %w", generationErr)
	}

	if result.LiveQuery == nil || result.LiveQuery.ChannelID != input.Subject() ||
		result.LiveQuery.PageCount != result.PageCount || result.LiveQuery.Exhausted != result.Exhausted ||
		result.LiveQuery.AccessRestricted != (len(result.UnavailableLiveSessions) > 0) {
		return nil, collecterr.New(contract.ErrorParserDrift, contract.ClassDataContract, "live query proof does not match helper pagination and restrictions")
	}

	payload, observed := liveSnapshotPayload(input.Subject(), result.LiveSessions, result.LiveQuery)

	envelope, err := subjectEnvelope(input, contract.KindLiveSnapshot, completeness, continuity, payload)
	if err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}

	envelope, err = collection.WithScheduleObservations(envelope, observed)
	if err != nil {
		return nil, fmt.Errorf("schedule observations: %w", err)
	}

	return &envelope, nil
}
