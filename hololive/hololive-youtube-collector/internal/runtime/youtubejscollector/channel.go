package youtubejscollector

import (
	"context"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

const (
	channelKindLive     = "live"
	channelKindMetadata = "metadata"
)

type ChannelClient interface {
	FetchChannel(ctx context.Context, request youtubejs.ChannelRequest) (youtubejs.ChannelResult, error)
}

type ChannelRunner struct {
	client ChannelClient
}

func NewChannelMetadataRunner(client ChannelClient) *ChannelRunner {
	return &ChannelRunner{client: client}
}

func (r *ChannelRunner) JobID() collection.JobID {
	return collection.JobID{Provider: contract.ProviderYouTubeJS, Kind: "youtubejs_channel_" + channelKindMetadata}
}

func (r *ChannelRunner) Collect(ctx context.Context, input *collection.RunInput) (collection.CollectResult, error) {
	if r == nil || r.client == nil {
		return collection.CollectResult{}, collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "youtube.js channel client is not configured")
	}

	if input == nil {
		return collection.CollectResult{}, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "collection run input is nil")
	}

	started := time.Now()

	enabled, err := enabledChannelKinds(input, input.Job().Emissions())
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("enabled channel kinds: %w", err)
	}

	if !enabled[contract.KindChannelProfile] && !enabled[contract.KindChannelPhoto] {
		out, completeErr := completeEmptyCollection(started)

		return out, completeErr
	}

	result, completeness, continuity, err := fetchChannelPage(ctx, r.client, input, channelKindMetadata)
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("fetch channel page: %w", err)
	}

	envelopes, err := channelMetadataEnvelopes(input, &result, enabled, completeness, continuity)
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("channel envelopes: %w", err)
	}

	out, err := collection.CompleteFromEnvelopes(envelopes, started)
	if err != nil {
		return out, fmt.Errorf("complete from envelopes: %w", err)
	}

	return out, nil
}

func completeEmptyCollection(started time.Time) (collection.CollectResult, error) {
	out, err := collection.CompleteFromEnvelopes(nil, started)
	if err != nil {
		return out, fmt.Errorf("complete from envelopes: %w", err)
	}

	return out, nil
}

func enabledChannelKinds(input *collection.RunInput, kinds []contract.ObservationKind) (map[contract.ObservationKind]bool, error) {
	enabled := make(map[contract.ObservationKind]bool, len(kinds))

	for _, kind := range kinds {
		allowed, err := input.Allows(kind, input.Subject())
		if err != nil {
			return nil, fmt.Errorf("allows: %w", err)
		}

		enabled[kind] = allowed
	}

	return enabled, nil
}

func fetchChannelPage(
	ctx context.Context,
	client ChannelClient,
	input *collection.RunInput,
	kind string,
) (youtubejs.ChannelResult, contract.Completeness, contract.Continuity, error) {
	result, err := client.FetchChannel(ctx, youtubejs.ChannelRequest{
		ChannelID:               input.Subject(),
		Kind:                    kind,
		MaxSuccessResponseBytes: input.MaxSuccessResponseBytes(),
		ScheduledFor:            new(input.Lease().ScheduledFor),
	})
	if err != nil {
		return youtubejs.ChannelResult{}, "", "", fmt.Errorf("fetch channel: %w", err)
	}

	if validateErr := validateLiveIdentity(input.Subject(), result.LiveSessions); validateErr != nil {
		return youtubejs.ChannelResult{}, "", "", fmt.Errorf("validate live identity: %w", validateErr)
	}

	if validateErr := validateUnavailableLiveSessions(input.Subject(), kind, &result); validateErr != nil {
		return youtubejs.ChannelResult{}, "", "", fmt.Errorf("validate unavailable live sessions: %w", validateErr)
	}

	completeness, continuity, err := PaginationOf(&result.Pagination)
	if err != nil {
		return youtubejs.ChannelResult{}, "", "", fmt.Errorf("pagination of: %w", err)
	}

	if len(result.UnavailableLiveSessions) > 0 {
		completeness = contract.CompletenessPartial
	}

	return result, completeness, continuity, nil
}

func channelMetadataEnvelopes(
	input *collection.RunInput,
	result *youtubejs.ChannelResult,
	enabled map[contract.ObservationKind]bool,
	completeness contract.Completeness,
	continuity contract.Continuity,
) ([]contract.Envelope, error) {
	subject := input.Subject()
	envelopes := make([]contract.Envelope, 0, 2)

	profile, ok := channelProfilePayload(subject, result.Profile)
	if err := appendBuiltEnvelope(input, contract.KindChannelProfile, enabled, completeness, continuity, profile, ok, &envelopes); err != nil {
		return nil, fmt.Errorf("append channel profile: %w", err)
	}

	photo, ok := channelPhotoPayload(subject, result.Photo)
	if err := appendBuiltEnvelope(input, contract.KindChannelPhoto, enabled, completeness, continuity, photo, ok, &envelopes); err != nil {
		return nil, fmt.Errorf("append channel photo: %w", err)
	}

	return envelopes, nil
}

func appendBuiltEnvelope(
	input *collection.RunInput,
	kind contract.ObservationKind,
	enabled map[contract.ObservationKind]bool,
	completeness contract.Completeness,
	continuity contract.Continuity,
	payload any,
	ok bool,
	envelopes *[]contract.Envelope,
) error {
	if !ok || !enabled[kind] {
		return nil
	}

	envelope, err := subjectEnvelope(input, kind, completeness, continuity, payload)
	if err != nil {
		return fmt.Errorf("envelope: %w", err)
	}

	*envelopes = append(*envelopes, envelope)

	return nil
}

func subjectEnvelope(
	input *collection.RunInput,
	kind contract.ObservationKind,
	completeness contract.Completeness,
	continuity contract.Continuity,
	payload any,
) (contract.Envelope, error) {
	generation, err := input.Generation(kind)
	if err != nil {
		return contract.Envelope{}, fmt.Errorf("generation: %w", err)
	}

	return generationEnvelope(input, kind, generation, completeness, continuity, payload)
}

func generationEnvelope(
	input *collection.RunInput,
	kind contract.ObservationKind,
	generation int64,
	completeness contract.Completeness,
	continuity contract.Continuity,
	payload any,
) (contract.Envelope, error) {
	lease := input.Lease()

	envelope, err := collection.Envelope(
		contract.ProviderYouTubeJS,
		kind,
		input.Subject(),
		generation,
		&lease,
		completeness,
		continuity,
		payload,
	)
	if err != nil {
		return contract.Envelope{}, fmt.Errorf("build collection envelope: %w", err)
	}

	return envelope, nil
}
