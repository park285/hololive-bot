package youtubejscollector

import (
	"context"
	"slices"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
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

func NewContentRunner(client ContentClient, maxResults int) *ContentRunner {
	return &ContentRunner{client: client, maxResults: collectutil.MaxResults(maxResults)}
}

func (r *ContentRunner) Provider() contract.Provider { return contract.ProviderYouTubeJS }
func (r *ContentRunner) JobKind() string             { return "youtubejs_content" }
func (r *ContentRunner) Emissions() []contract.ObservationKind {
	return []contract.ObservationKind{contract.KindVideoList, contract.KindShortsList}
}
func (r *ContentRunner) TargetKinds() []contract.ObservationKind { return r.Emissions() }

func (r *ContentRunner) Collect(ctx context.Context, input *collectutil.RunInput) (collectutil.RunOutput, error) {
	if r == nil || r.client == nil {
		return collectutil.RunOutput{}, collecterr.New(collecterr.Failed, "youtube.js content client is not configured")
	}
	if err := collectutil.ValidateInput(input); err != nil {
		return collectutil.RunOutput{}, err
	}
	started := time.Now()
	envelopes, err := r.collectEnabledContent(ctx, input)
	if err != nil {
		return collectutil.RunOutput{}, err
	}
	return collectutil.Output(envelopes, started)
}

func (r *ContentRunner) collectEnabledContent(ctx context.Context, input *collectutil.RunInput) ([]contract.Envelope, error) {
	envelopes := make([]contract.Envelope, 0, 2)
	if err := r.appendContentKind(ctx, input, contract.KindVideoList, "videos", &envelopes); err != nil {
		return nil, err
	}
	if err := r.appendContentKind(ctx, input, contract.KindShortsList, "shorts", &envelopes); err != nil {
		return nil, err
	}
	return envelopes, nil
}

func (r *ContentRunner) appendContentKind(
	ctx context.Context,
	input *collectutil.RunInput,
	kind contract.ObservationKind,
	tab string,
	envelopes *[]contract.Envelope,
) error {
	if !subjectEnabled(input, kind) {
		return nil
	}
	item, err := r.fetchKind(ctx, input, tab)
	if err != nil {
		return err
	}
	if item != nil {
		*envelopes = append(*envelopes, *item)
	}
	return nil
}

func subjectEnabled(input *collectutil.RunInput, kind contract.ObservationKind) bool {
	subjects, configured := input.EnabledSubjects[kind]
	if !configured {
		return true
	}
	return slices.Contains(subjects, input.Spec.SubjectKey)
}

func (r *ContentRunner) fetchKind(ctx context.Context, input *collectutil.RunInput, kind string) (*contract.Envelope, error) {
	result, err := r.client.FetchContent(ctx, youtubejs.ContentRequest{
		ChannelID:         input.Spec.SubjectKey,
		Kind:              kind,
		MaxResults:        r.maxResults,
		MaxPages:          input.MaxPages,
		MaxAggregateBytes: input.MaxAggregateBytes,
	})
	if err != nil {
		return nil, err
	}
	if result.MissingTab {
		return nil, nil
	}
	observationKind := contract.KindVideoList
	if kind == "shorts" {
		observationKind = contract.KindShortsList
	}
	generation, err := collectutil.Generation(input, observationKind)
	if err != nil {
		return nil, err
	}
	completeness, continuity, err := collectutil.PaginationOf(result.Pagination)
	if err != nil {
		return nil, err
	}
	videos, shorts := videoListPayload(input.Spec.SubjectKey, result.Items, r.maxResults, result.Pagination, kind == "shorts")
	var payload any = videos
	if kind == "shorts" {
		payload = shorts
	}
	envelope, err := collectutil.Envelope(
		contract.ProviderYouTubeJS,
		observationKind,
		input.Spec.SubjectKey,
		generation,
		&input.Lease,
		completeness,
		continuity,
		payload,
	)
	if err != nil {
		return nil, collecterr.Wrap(collecterr.ParserDrift, err)
	}
	return &envelope, nil
}
