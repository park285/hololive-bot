package youtubejscollector

import (
	"context"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
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

func (r *CommunityRunner) Provider() contract.Provider { return contract.ProviderYouTubeJS }
func (r *CommunityRunner) JobKind() string             { return "community_collect" }
func (r *CommunityRunner) Emissions() []contract.ObservationKind {
	return []contract.ObservationKind{contract.KindCommunityPage}
}

func (r *CommunityRunner) Collect(ctx context.Context, input collectutil.RunInput) (collectutil.RunOutput, error) {
	if r == nil || r.client == nil {
		return collectutil.RunOutput{}, collecterr.New(collecterr.Failed, "youtube.js community client is not configured")
	}
	started := time.Now()
	result, err := r.client.FetchCommunity(ctx, youtubejs.CommunityRequest{
		ChannelID:         input.Spec.SubjectKey,
		MaxResults:        r.maxResults,
		MaxPages:          input.MaxPages,
		MaxAggregateBytes: input.MaxAggregateBytes,
	})
	if err != nil {
		return collectutil.RunOutput{}, err
	}
	if result.MissingTab {
		return collectutil.Output(nil, started)
	}
	generation, err := collectutil.Generation(input, contract.KindCommunityPage)
	if err != nil {
		return collectutil.RunOutput{}, err
	}
	completeness, continuity, err := collectutil.PaginationOf(result.Pagination)
	if err != nil {
		return collectutil.RunOutput{}, err
	}
	envelope, err := collectutil.Envelope(
		contract.ProviderYouTubeJS,
		contract.KindCommunityPage,
		input.Spec.SubjectKey,
		generation,
		input.Lease,
		completeness,
		continuity,
		communityPayload(input.Spec.SubjectKey, result.Posts, r.maxResults, result.Pagination),
	)
	if err != nil {
		return collectutil.RunOutput{}, collecterr.Wrap(collecterr.ParserDrift, err)
	}
	return collectutil.Output([]contract.Envelope{envelope}, started)
}
