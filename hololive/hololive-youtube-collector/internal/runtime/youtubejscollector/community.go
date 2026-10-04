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

type CommunityClient interface {
	FetchCommunity(ctx context.Context, request youtubejs.CommunityRequest) (youtubejs.CommunityResult, error)
}

type CommunityRunner struct {
	client CommunityClient
}

func NewCommunityRunner(client CommunityClient) *CommunityRunner {
	return &CommunityRunner{client: client}
}

func (r *CommunityRunner) JobID() collection.JobID {
	return collection.JobID{Provider: contract.ProviderYouTubeJS, Kind: "community_collect"}
}

func (r *CommunityRunner) Collect(ctx context.Context, input *collection.RunInput) (collection.CollectResult, error) {
	if invalidCommunityRunner(r) {
		return collection.CollectResult{}, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "youtube.js community client is not configured")
	}

	if input == nil {
		return collection.CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is nil")
	}

	started := time.Now()

	result, err := r.client.FetchCommunity(ctx, youtubejs.CommunityRequest{
		ChannelID:               input.Subject(),
		MaxResults:              maxResultsPerPage,
		MaxPages:                input.MaxPages(),
		MaxSuccessResponseBytes: input.MaxSuccessResponseBytes(),
	})
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("fetch community: %w", err)
	}

	if validateErr := validateCommunityRows(result.Posts); validateErr != nil {
		return collection.CollectResult{}, fmt.Errorf("validate community rows: %w", validateErr)
	}

	if result.MissingTab {
		out, completeErr := completeEmptyCollection(started)

		return out, completeErr
	}

	envelope, err := r.communityEnvelope(input, &result)
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("community envelope: %w", err)
	}

	out, err := collection.CompleteFromEnvelopes([]contract.Envelope{envelope}, started)
	if err != nil {
		return out, fmt.Errorf("complete from envelopes: %w", err)
	}

	return out, nil
}

func invalidCommunityRunner(r *CommunityRunner) bool {
	return r == nil || r.client == nil
}

func (r *CommunityRunner) communityEnvelope(input *collection.RunInput, result *youtubejs.CommunityResult) (contract.Envelope, error) {
	generation, err := input.Generation(contract.KindCommunityPage)
	if err != nil {
		return contract.Envelope{}, fmt.Errorf("generation: %w", err)
	}

	completeness, continuity, err := PaginationOf(&result.Pagination)
	if err != nil {
		return contract.Envelope{}, fmt.Errorf("pagination of: %w", err)
	}

	payload := communityPayload(input.Subject(), result.Posts, maxResultsPerPage, &result.Pagination)

	return generationEnvelope(input, contract.KindCommunityPage, generation, completeness, continuity, payload)
}
