package youtubejscollector

import (
	"context"
	"errors"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

type CommunityClient interface {
	FetchCommunity(ctx context.Context, request youtubejs.CommunityRequest) (youtubejs.CommunityResult, error)
}

type CommunityRunner struct {
	client     CommunityClient
	maxResults int
}

func NewCommunityRunner(client CommunityClient, maxResults int) *CommunityRunner {
	return &CommunityRunner{client: client, maxResults: collectutil.MaxResults(maxResults)}
}

func (r *CommunityRunner) JobID() sourceobservation.JobID {
	return sourceobservation.JobID{Provider: contract.ProviderYouTubeJS, Kind: "community_collect"}
}

func (r *CommunityRunner) Collect(ctx context.Context, input *collectutil.RunInput) (collectutil.CollectResult, error) {
	if invalidCommunityRunner(r) {
		return collectutil.CollectResult{}, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "youtube.js community client is not configured")
	}

	if input == nil {
		return collectutil.CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is nil")
	}

	started := time.Now()
	spec := input.Spec()

	result, err := r.client.FetchCommunity(ctx, youtubejs.CommunityRequest{
		ChannelID:               spec.SubjectKey,
		MaxResults:              r.maxResults,
		MaxPages:                input.MaxPages(),
		MaxSuccessResponseBytes: input.MaxSuccessResponseBytes(),
	})
	if err != nil {
		return collectutil.CollectResult{}, fmt.Errorf("fetch community: %w", err)
	}

	if validateErr := validateCommunityRows(result.Posts); validateErr != nil {
		return collectutil.CollectResult{}, fmt.Errorf("validate community rows: %w", validateErr)
	}

	if result.MissingTab {
		out, completeErr := completeEmptyCollection(started)

		return out, errors.Join(completeErr)
	}

	envelope, err := r.communityEnvelope(input, &result)
	if err != nil {
		return collectutil.CollectResult{}, fmt.Errorf("community envelope: %w", err)
	}

	out, err := collectutil.CompleteFromEnvelopes([]contract.Envelope{envelope}, started)
	if err != nil {
		return out, fmt.Errorf("complete from envelopes: %w", err)
	}

	return out, nil
}

func invalidCommunityRunner(r *CommunityRunner) bool {
	return r == nil || r.client == nil
}

func (r *CommunityRunner) communityEnvelope(input *collectutil.RunInput, result *youtubejs.CommunityResult) (contract.Envelope, error) {
	spec := input.Spec()

	generation, err := input.Generation(contract.KindCommunityPage)
	if err != nil {
		return contract.Envelope{}, fmt.Errorf("generation: %w", err)
	}

	completeness, continuity, err := collectutil.PaginationOf(&result.Pagination)
	if err != nil {
		return contract.Envelope{}, fmt.Errorf("pagination of: %w", err)
	}

	lease := input.Lease()

	envelope, err := collectutil.Envelope(
		contract.ProviderYouTubeJS,
		contract.KindCommunityPage,
		spec.SubjectKey,
		generation,
		&lease,
		completeness,
		continuity,
		communityPayload(spec.SubjectKey, result.Posts, r.maxResults, &result.Pagination),
	)
	if err != nil {
		return contract.Envelope{}, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, err)
	}

	return envelope, nil
}
