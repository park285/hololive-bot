package youtubejscollector

import (
	"context"
	"strings"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

type ViewerClient interface {
	FetchViewer(ctx context.Context, request youtubejs.ViewerRequest) (youtubejs.ViewerResult, error)
}

type ViewerRunner struct {
	client ViewerClient
}

func NewViewerRunner(client ViewerClient) *ViewerRunner {
	return &ViewerRunner{client: client}
}

func (r *ViewerRunner) Provider() contract.Provider { return contract.ProviderYouTubeJS }
func (r *ViewerRunner) JobKind() string             { return "youtubejs_viewer" }
func (r *ViewerRunner) Emissions() []contract.ObservationKind {
	return []contract.ObservationKind{contract.KindViewerSample}
}
func (r *ViewerRunner) TargetKinds() []contract.ObservationKind { return r.Emissions() }

func (r *ViewerRunner) Collect(ctx context.Context, input collectutil.RunInput) (collectutil.RunOutput, error) {
	if r == nil || r.client == nil {
		return collectutil.RunOutput{}, collecterr.New(collecterr.Failed, "youtube.js viewer client is not configured")
	}
	if looksLikeYouTubeChannelID(input.Spec.SubjectKey) {
		return collectutil.RunOutput{}, collecterr.New(collecterr.Failed, "viewer_sample subject must be a video id")
	}
	started := time.Now()
	result, err := r.client.FetchViewer(ctx, youtubejs.ViewerRequest{
		VideoID:           input.Spec.SubjectKey,
		MaxAggregateBytes: input.MaxAggregateBytes,
	})
	if err != nil {
		return collectutil.RunOutput{}, err
	}
	generation, err := collectutil.Generation(input, contract.KindViewerSample)
	if err != nil {
		return collectutil.RunOutput{}, err
	}
	windowStart := input.Lease.ScheduledFor.UTC()
	windowSeconds := collectutil.SampleWindowSeconds(input.Spec.PollInterval)
	envelope, err := collectutil.Envelope(
		contract.ProviderYouTubeJS,
		contract.KindViewerSample,
		input.Spec.SubjectKey,
		generation,
		input.Lease,
		contract.CompletenessComplete,
		contract.ContinuityNotApplicable,
		viewerPayload(input.Spec.SubjectKey, result, windowStart, windowSeconds),
	)
	if err != nil {
		return collectutil.RunOutput{}, collecterr.Wrap(collecterr.ParserDrift, err)
	}
	return collectutil.Output([]contract.Envelope{envelope}, started)
}

func looksLikeYouTubeChannelID(value string) bool {
	id := strings.TrimSpace(value)
	return strings.HasPrefix(id, "UC") && len(id) >= 22
}
