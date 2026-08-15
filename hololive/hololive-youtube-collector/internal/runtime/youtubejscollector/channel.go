package youtubejscollector

import (
	"context"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

type ChannelClient interface {
	FetchChannel(ctx context.Context, request youtubejs.ChannelRequest) (youtubejs.ChannelResult, error)
}

type ChannelRunner struct {
	client    ChannelClient
	jobKind   string
	emissions []contract.ObservationKind
}

func NewChannelLiveRunner(client ChannelClient) *ChannelRunner {
	return &ChannelRunner{
		client:    client,
		jobKind:   "youtubejs_channel_live",
		emissions: []contract.ObservationKind{contract.KindLiveSnapshot},
	}
}

func NewChannelMetadataRunner(client ChannelClient) *ChannelRunner {
	return &ChannelRunner{
		client:  client,
		jobKind: "youtubejs_channel_metadata",
		emissions: []contract.ObservationKind{
			contract.KindChannelStats,
			contract.KindChannelProfile,
			contract.KindChannelPhoto,
		},
	}
}

func (r *ChannelRunner) Provider() contract.Provider { return contract.ProviderYouTubeJS }
func (r *ChannelRunner) JobKind() string             { return r.jobKind }
func (r *ChannelRunner) Emissions() []contract.ObservationKind {
	return append([]contract.ObservationKind(nil), r.emissions...)
}
func (r *ChannelRunner) TargetKinds() []contract.ObservationKind { return r.Emissions() }

func (r *ChannelRunner) Collect(ctx context.Context, input *collectutil.RunInput) (collectutil.RunOutput, error) {
	if r == nil || r.client == nil {
		return collectutil.RunOutput{}, collecterr.New(collecterr.Failed, "youtube.js channel client is not configured")
	}
	if err := collectutil.ValidateInput(input); err != nil {
		return collectutil.RunOutput{}, err
	}
	started := time.Now()
	enabled := enabledChannelKinds(input, r.Emissions())
	if !anyChannelKindEnabled(enabled) {
		return collectutil.Output(nil, started)
	}
	result, completeness, continuity, err := r.fetchChannelPage(ctx, input)
	if err != nil {
		return collectutil.RunOutput{}, err
	}
	envelopes, err := r.channelEnvelopes(input, &result, enabled, completeness, continuity)
	if err != nil {
		return collectutil.RunOutput{}, err
	}
	return collectutil.Output(envelopes, started)
}

func enabledChannelKinds(input *collectutil.RunInput, kinds []contract.ObservationKind) map[contract.ObservationKind]bool {
	enabled := make(map[contract.ObservationKind]bool, len(kinds))
	for _, kind := range kinds {
		enabled[kind] = subjectEnabled(input, kind)
	}
	return enabled
}

func anyChannelKindEnabled(enabled map[contract.ObservationKind]bool) bool {
	return enabled[contract.KindLiveSnapshot] || enabled[contract.KindChannelStats] ||
		enabled[contract.KindChannelProfile] || enabled[contract.KindChannelPhoto]
}

func (r *ChannelRunner) fetchChannelPage(ctx context.Context, input *collectutil.RunInput) (youtubejs.ChannelResult, contract.Completeness, contract.Continuity, error) {
	result, err := r.client.FetchChannel(ctx, youtubejs.ChannelRequest{
		ChannelID:         input.Spec.SubjectKey,
		MaxPages:          input.MaxPages,
		MaxAggregateBytes: input.MaxAggregateBytes,
	})
	if err != nil {
		return youtubejs.ChannelResult{}, "", "", err
	}
	completeness, continuity, err := collectutil.PaginationOf(result.Pagination)
	if err != nil {
		return youtubejs.ChannelResult{}, "", "", err
	}
	return result, completeness, continuity, nil
}

func (r *ChannelRunner) channelEnvelopes(
	input *collectutil.RunInput,
	result *youtubejs.ChannelResult,
	enabled map[contract.ObservationKind]bool,
	completeness contract.Completeness,
	continuity contract.Continuity,
) ([]contract.Envelope, error) {
	envelopes := make([]contract.Envelope, 0, 4)
	if err := r.appendLiveEnvelope(input, result, enabled, completeness, continuity, &envelopes); err != nil {
		return nil, err
	}
	if err := r.appendChannelStats(input, result, enabled, completeness, continuity, &envelopes); err != nil {
		return nil, err
	}
	if err := r.appendChannelProfile(input, result, enabled, completeness, continuity, &envelopes); err != nil {
		return nil, err
	}
	if err := r.appendChannelPhoto(input, result, enabled, completeness, continuity, &envelopes); err != nil {
		return nil, err
	}
	return envelopes, nil
}

func (r *ChannelRunner) appendLiveEnvelope(
	input *collectutil.RunInput,
	result *youtubejs.ChannelResult,
	enabled map[contract.ObservationKind]bool,
	completeness contract.Completeness,
	continuity contract.Continuity,
	envelopes *[]contract.Envelope,
) error {
	if !enabled[contract.KindLiveSnapshot] {
		return nil
	}
	live, err := r.envelope(input, contract.KindLiveSnapshot, completeness, continuity, liveSnapshotPayload(input.Spec.SubjectKey, result.LiveSessions))
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, live)
	return nil
}

func (r *ChannelRunner) appendChannelStats(
	input *collectutil.RunInput,
	result *youtubejs.ChannelResult,
	enabled map[contract.ObservationKind]bool,
	completeness contract.Completeness,
	continuity contract.Continuity,
	envelopes *[]contract.Envelope,
) error {
	payload, ok := channelStatsPayload(input.Spec.SubjectKey, result.Stats)
	return r.appendBuiltEnvelope(input, contract.KindChannelStats, enabled, completeness, continuity, payload, ok, envelopes)
}

func (r *ChannelRunner) appendChannelProfile(
	input *collectutil.RunInput,
	result *youtubejs.ChannelResult,
	enabled map[contract.ObservationKind]bool,
	completeness contract.Completeness,
	continuity contract.Continuity,
	envelopes *[]contract.Envelope,
) error {
	payload, ok := channelProfilePayload(input.Spec.SubjectKey, result.Profile)
	return r.appendBuiltEnvelope(input, contract.KindChannelProfile, enabled, completeness, continuity, payload, ok, envelopes)
}

func (r *ChannelRunner) appendChannelPhoto(
	input *collectutil.RunInput,
	result *youtubejs.ChannelResult,
	enabled map[contract.ObservationKind]bool,
	completeness contract.Completeness,
	continuity contract.Continuity,
	envelopes *[]contract.Envelope,
) error {
	payload, ok := channelPhotoPayload(input.Spec.SubjectKey, result.Photo)
	return r.appendBuiltEnvelope(input, contract.KindChannelPhoto, enabled, completeness, continuity, payload, ok, envelopes)
}

func (r *ChannelRunner) appendBuiltEnvelope(
	input *collectutil.RunInput,
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
	envelope, err := r.envelope(input, kind, completeness, continuity, payload)
	if err != nil {
		return err
	}
	*envelopes = append(*envelopes, envelope)
	return nil
}

func (r *ChannelRunner) envelope(
	input *collectutil.RunInput,
	kind contract.ObservationKind,
	completeness contract.Completeness,
	continuity contract.Continuity,
	payload any,
) (contract.Envelope, error) {
	generation, err := collectutil.Generation(input, kind)
	if err != nil {
		return contract.Envelope{}, err
	}
	envelope, err := collectutil.Envelope(
		contract.ProviderYouTubeJS,
		kind,
		input.Spec.SubjectKey,
		generation,
		&input.Lease,
		completeness,
		continuity,
		payload,
	)
	if err != nil {
		return contract.Envelope{}, collecterr.Wrap(collecterr.ParserDrift, err)
	}
	return envelope, nil
}
