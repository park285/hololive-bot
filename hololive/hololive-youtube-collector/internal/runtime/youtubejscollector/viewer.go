package youtubejscollector

import (
	"context"
	"strings"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
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

func (r *ViewerRunner) JobID() sourceobservation.JobID {
	return sourceobservation.JobID{Provider: contract.ProviderYouTubeJS, Kind: "youtubejs_viewer"}
}

func (r *ViewerRunner) Collect(ctx context.Context, input *collectutil.RunInput) (collectutil.CollectResult, error) {
	if r == nil || r.client == nil {
		return collectutil.CollectResult{}, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "youtube.js viewer client is not configured")
	}
	if input == nil {
		return collectutil.CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is nil")
	}
	spec := input.Spec()
	if looksLikeYouTubeChannelID(spec.SubjectKey) {
		return collectutil.CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "viewer_sample subject must be a video id")
	}
	started := time.Now()
	result, err := r.client.FetchViewer(ctx, youtubejs.ViewerRequest{
		VideoID:                 spec.SubjectKey,
		MaxSuccessResponseBytes: input.MaxSuccessResponseBytes(),
	})
	if err != nil {
		return collectutil.CollectResult{}, err
	}
	if err := validateViewerIdentity(spec.SubjectKey, &result); err != nil {
		return collectutil.CollectResult{}, err
	}
	generation, err := input.Generation(contract.KindViewerSample)
	if err != nil {
		return collectutil.CollectResult{}, err
	}
	lease := input.Lease()
	windowStart := lease.ScheduledFor.UTC()
	windowSeconds := collectutil.SampleWindowSeconds(spec.PollInterval)
	envelope, err := collectutil.Envelope(
		contract.ProviderYouTubeJS,
		contract.KindViewerSample,
		spec.SubjectKey,
		generation,
		&lease,
		contract.CompletenessComplete,
		contract.ContinuityNotApplicable,
		viewerPayload(spec.SubjectKey, &result, windowStart, windowSeconds),
	)
	if err != nil {
		return collectutil.CollectResult{}, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, err)
	}
	return collectutil.CompleteFromEnvelopes([]contract.Envelope{envelope}, started)
}

func looksLikeYouTubeChannelID(value string) bool {
	id := strings.TrimSpace(value)
	return strings.HasPrefix(id, "UC") && len(id) >= 22
}
