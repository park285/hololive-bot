package youtubejscollector

import (
	"context"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

type ContentClient interface {
	FetchContent(ctx context.Context, request youtubejs.ContentRequest) (youtubejs.ContentResult, error)
}

type ContentRunner struct {
	client     ContentClient
	maxResults int
}

type contentKind struct {
	kind contract.ObservationKind
	tab  string
}

var contentKinds = []contentKind{
	{kind: contract.KindVideoList, tab: "videos"},
	{kind: contract.KindShortsList, tab: "shorts"},
}

func NewContentRunner(client ContentClient, maxResults int) *ContentRunner {
	return &ContentRunner{client: client, maxResults: collectutil.MaxResults(maxResults)}
}

func (r *ContentRunner) JobID() sourceobservation.JobID {
	return sourceobservation.JobID{Provider: contract.ProviderYouTubeJS, Kind: "youtubejs_content"}
}

func (r *ContentRunner) Collect(ctx context.Context, input *collectutil.RunInput) (collectutil.CollectResult, error) {
	if r == nil || r.client == nil {
		return collectutil.CollectResult{}, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "youtube.js content client is not configured")
	}
	if input == nil {
		return collectutil.CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is nil")
	}
	return r.collectAllowedKinds(ctx, input, time.Now())
}

func (r *ContentRunner) collectAllowedKinds(
	ctx context.Context,
	input *collectutil.RunInput,
	started time.Time,
) (collectutil.CollectResult, error) {
	envelopes := make([]contract.Envelope, 0, 2)
	for _, item := range contentKinds {
		envelope, err := r.collectKind(ctx, input, item)
		if err != nil {
			return partialContentResult(ctx, envelopes, started, item.kind, err)
		}
		if envelope != nil {
			envelopes = append(envelopes, *envelope)
		}
	}
	return collectutil.CompleteFromEnvelopes(envelopes, started)
}

func (r *ContentRunner) collectKind(ctx context.Context, input *collectutil.RunInput, item contentKind) (*contract.Envelope, error) {
	allowed, err := input.Allows(item.kind, input.Spec().SubjectKey)
	if err != nil || !allowed {
		return nil, err
	}
	return r.fetchKind(ctx, input, item.kind, item.tab)
}

func partialContentResult(
	ctx context.Context,
	envelopes []contract.Envelope,
	started time.Time,
	kind contract.ObservationKind,
	err error,
) (collectutil.CollectResult, error) {
	if ctx.Err() != nil {
		return collectutil.CollectResult{}, ctx.Err()
	}
	if len(envelopes) == 0 || !contentPartialFailureAllowed(collecterr.ClassOf(err)) {
		return collectutil.CollectResult{}, err
	}
	output, buildErr := collectutil.OutputFromEnvelopes(envelopes, started)
	if buildErr != nil {
		return collectutil.CollectResult{}, buildErr
	}
	return collectutil.NewPartialResult(output, collecterr.Normalize(err), kind)
}

func (r *ContentRunner) fetchKind(
	ctx context.Context,
	input *collectutil.RunInput,
	observationKind contract.ObservationKind,
	tab string,
) (*contract.Envelope, error) {
	spec := input.Spec()
	result, err := r.client.FetchContent(ctx, youtubejs.ContentRequest{
		ChannelID:               spec.SubjectKey,
		Kind:                    tab,
		MaxResults:              r.maxResults,
		MaxPages:                input.MaxPages(),
		MaxSuccessResponseBytes: input.MaxSuccessResponseBytes(),
	})
	if err != nil {
		return nil, err
	}
	if err := validateContentIdentity(spec.SubjectKey, result.Items); err != nil {
		return nil, err
	}
	if result.MissingTab {
		return nil, nil
	}
	generation, err := input.Generation(observationKind)
	if err != nil {
		return nil, err
	}
	completeness, continuity, err := collectutil.PaginationOf(&result.Pagination)
	if err != nil {
		return nil, err
	}
	videos, shorts := videoListPayload(spec.SubjectKey, result.Items, r.maxResults, &result.Pagination, tab == "shorts")
	var payload any = videos
	if tab == "shorts" {
		payload = shorts
	}
	lease := input.Lease()
	envelope, err := collectutil.Envelope(
		contract.ProviderYouTubeJS,
		observationKind,
		spec.SubjectKey,
		generation,
		&lease,
		completeness,
		continuity,
		payload,
	)
	if err != nil {
		return nil, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, err)
	}
	return &envelope, nil
}
