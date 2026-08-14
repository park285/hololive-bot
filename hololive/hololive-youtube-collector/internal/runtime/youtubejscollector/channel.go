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
	client ChannelClient
}

func NewChannelRunner(client ChannelClient) *ChannelRunner {
	return &ChannelRunner{client: client}
}

func (r *ChannelRunner) Provider() contract.Provider { return contract.ProviderYouTubeJS }
func (r *ChannelRunner) JobKind() string             { return "youtubejs_channel" }
func (r *ChannelRunner) Emissions() []contract.ObservationKind {
	return []contract.ObservationKind{
		contract.KindLiveSnapshot,
		contract.KindChannelStats,
		contract.KindChannelProfile,
		contract.KindChannelPhoto,
	}
}

func (r *ChannelRunner) Collect(ctx context.Context, input collectutil.RunInput) (collectutil.RunOutput, error) {
	if r == nil || r.client == nil {
		return collectutil.RunOutput{}, collecterr.New(collecterr.Failed, "youtube.js channel client is not configured")
	}
	started := time.Now()
	enabled := make(map[contract.ObservationKind]bool, len(r.Emissions()))
	for _, kind := range r.Emissions() {
		enabled[kind] = subjectEnabled(input, kind)
	}
	if !enabled[contract.KindLiveSnapshot] && !enabled[contract.KindChannelStats] &&
		!enabled[contract.KindChannelProfile] && !enabled[contract.KindChannelPhoto] {
		return collectutil.Output(nil, started)
	}
	result, err := r.client.FetchChannel(ctx, youtubejs.ChannelRequest{
		ChannelID:         input.Spec.SubjectKey,
		MaxPages:          input.MaxPages,
		MaxAggregateBytes: input.MaxAggregateBytes,
	})
	if err != nil {
		return collectutil.RunOutput{}, err
	}
	completeness, continuity, err := collectutil.PaginationOf(result.Pagination)
	if err != nil {
		return collectutil.RunOutput{}, err
	}
	envelopes := make([]contract.Envelope, 0, 4)
	if enabled[contract.KindLiveSnapshot] {
		live, err := r.envelope(input, contract.KindLiveSnapshot, completeness, continuity, liveSnapshotPayload(input.Spec.SubjectKey, result.LiveSessions))
		if err != nil {
			return collectutil.RunOutput{}, err
		}
		envelopes = append(envelopes, live)
	}
	if stats, ok := channelStatsPayload(input.Spec.SubjectKey, result.Stats); ok && enabled[contract.KindChannelStats] {
		envelope, err := r.envelope(input, contract.KindChannelStats, completeness, continuity, stats)
		if err != nil {
			return collectutil.RunOutput{}, err
		}
		envelopes = append(envelopes, envelope)
	}
	if profile, ok := channelProfilePayload(input.Spec.SubjectKey, result.Profile); ok && enabled[contract.KindChannelProfile] {
		envelope, err := r.envelope(input, contract.KindChannelProfile, completeness, continuity, profile)
		if err != nil {
			return collectutil.RunOutput{}, err
		}
		envelopes = append(envelopes, envelope)
	}
	if photo, ok := channelPhotoPayload(input.Spec.SubjectKey, result.Photo); ok && enabled[contract.KindChannelPhoto] {
		envelope, err := r.envelope(input, contract.KindChannelPhoto, completeness, continuity, photo)
		if err != nil {
			return collectutil.RunOutput{}, err
		}
		envelopes = append(envelopes, envelope)
	}
	return collectutil.Output(envelopes, started)
}

func (r *ChannelRunner) envelope(
	input collectutil.RunInput,
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
		input.Lease,
		completeness,
		continuity,
		payload,
	)
	if err != nil {
		return contract.Envelope{}, collecterr.Wrap(collecterr.ParserDrift, err)
	}
	return envelope, nil
}
