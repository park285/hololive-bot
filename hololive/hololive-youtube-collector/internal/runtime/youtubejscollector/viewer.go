package youtubejscollector

import (
	"context"
	"fmt"
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
	if invalidViewerRunner(r) {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return collectutil.CollectResult{}, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "youtube.js viewer client is not configured")
	}

	if input == nil {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return collectutil.CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is nil")
	}

	spec := input.Spec()
	if looksLikeYouTubeChannelID(spec.SubjectKey) {
		//nolint:wrapcheck // 오류 생성자가 만든 값이라 감쌀 하위 오류가 없다.
		return collectutil.CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "viewer_sample subject must be a video id")
	}

	started := time.Now()

	result, err := r.client.FetchViewer(ctx, youtubejs.ViewerRequest{
		VideoID:                 spec.SubjectKey,
		MaxSuccessResponseBytes: input.MaxSuccessResponseBytes(),
	})
	if err != nil {
		return collectutil.CollectResult{}, fmt.Errorf("fetch viewer: %w", err)
	}

	if validateErr := validateViewerIdentity(spec.SubjectKey, &result); validateErr != nil {
		return collectutil.CollectResult{}, fmt.Errorf("validate viewer identity: %w", validateErr)
	}

	generation, err := input.Generation(contract.KindViewerSample)
	if err != nil {
		return collectutil.CollectResult{}, fmt.Errorf("generation: %w", err)
	}

	envelope, err := viewerEnvelope(input, &result, generation)
	if err != nil {
		return collectutil.CollectResult{}, fmt.Errorf("%w", err)
	}

	out, err := collectutil.CompleteFromEnvelopes([]contract.Envelope{envelope}, started)
	if err != nil {
		return out, fmt.Errorf("complete from envelopes: %w", err)
	}

	return out, nil
}

func invalidViewerRunner(r *ViewerRunner) bool {
	return r == nil || r.client == nil
}

func viewerEnvelope(input *collectutil.RunInput, result *youtubejs.ViewerResult, generation int64) (contract.Envelope, error) {
	spec := input.Spec()
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
		viewerPayload(spec.SubjectKey, result, windowStart, windowSeconds),
	)
	if err != nil {
		return contract.Envelope{}, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, err)
	}

	return envelope, nil
}

func looksLikeYouTubeChannelID(value string) bool {
	id := strings.TrimSpace(value)
	return strings.HasPrefix(id, "UC") && len(id) >= 22
}
