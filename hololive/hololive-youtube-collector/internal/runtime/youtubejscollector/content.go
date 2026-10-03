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

type ContentClient interface {
	FetchContent(ctx context.Context, request youtubejs.ContentRequest) (youtubejs.ContentResult, error)
}

type ContentRunner struct {
	client ContentClient
}

type contentKind struct {
	kind contract.ObservationKind
	tab  string
}

const (
	contentTabVideos = "videos"
	contentTabShorts = "shorts"
)

var contentKinds = []contentKind{
	{kind: contract.KindVideoList, tab: contentTabVideos},
	{kind: contract.KindShortsList, tab: contentTabShorts},
}

func NewContentRunner(client ContentClient) *ContentRunner {
	return &ContentRunner{client: client}
}

func (r *ContentRunner) JobID() collection.JobID {
	return collection.JobID{Provider: contract.ProviderYouTubeJS, Kind: "youtubejs_content"}
}

func (r *ContentRunner) Collect(ctx context.Context, input *collection.RunInput) (collection.CollectResult, error) {
	if r == nil || r.client == nil {
		return collection.CollectResult{}, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "youtube.js content client is not configured")
	}

	if input == nil {
		return collection.CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is nil")
	}

	out, err := r.collectAllowedKinds(ctx, input, time.Now())
	if err != nil {
		return out, fmt.Errorf("collect allowed kinds: %w", err)
	}

	return out, nil
}

func (r *ContentRunner) collectAllowedKinds(
	ctx context.Context,
	input *collection.RunInput,
	started time.Time,
) (collection.CollectResult, error) {
	envelopes := make([]contract.Envelope, 0, 2)

	for _, item := range contentKinds {
		envelope, err := r.collectKind(ctx, input, item)
		if err != nil {
			out, partialErr := partialContentResultForError(ctx, envelopes, started, item.kind, err)

			return out, partialErr
		}

		if envelope != nil {
			envelopes = append(envelopes, *envelope)
		}
	}

	out, err := collection.CompleteFromEnvelopes(envelopes, started)
	if err != nil {
		return out, fmt.Errorf("complete from envelopes: %w", err)
	}

	return out, nil
}

func partialContentResultForError(ctx context.Context, envelopes []contract.Envelope, started time.Time, kind contract.ObservationKind, cause error) (collection.CollectResult, error) {
	out, err := partialContentResult(ctx, envelopes, started, kind, cause)
	if err != nil {
		return out, fmt.Errorf("partial content result: %w", err)
	}

	return out, nil
}

func (r *ContentRunner) collectKind(ctx context.Context, input *collection.RunInput, item contentKind) (*contract.Envelope, error) {
	allowed, err := input.Allows(item.kind, input.Subject())
	if err != nil {
		return nil, fmt.Errorf("allows: %w", err)
	}

	if !allowed {
		//nolint:nilnil // 수집 대상이 아니면 봉투 없이 건너뛴다는 뜻이라 오류가 아니다.
		return nil, nil
	}

	out, err := r.fetchKind(ctx, input, item.kind, item.tab)
	if err != nil {
		return nil, fmt.Errorf("fetch kind: %w", err)
	}

	return out, nil
}

func partialContentResult(
	ctx context.Context,
	envelopes []contract.Envelope,
	started time.Time,
	kind contract.ObservationKind,
	err error,
) (collection.CollectResult, error) {
	if ctx.Err() != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return collection.CollectResult{}, fmt.Errorf("collect content: %w", ctxErr)
		}

		return collection.CollectResult{}, nil
	}

	if len(envelopes) == 0 || !contentPartialFailureAllowed(collecterr.ClassOf(err)) {
		return collection.CollectResult{}, err
	}

	output, buildErr := collection.OutputFromEnvelopes(envelopes, started)
	if buildErr != nil {
		return collection.CollectResult{}, fmt.Errorf("output from envelopes: %w", buildErr)
	}

	out, err := collection.NewPartialResult(output, collecterr.Normalize(err), kind)
	if err != nil {
		return out, fmt.Errorf("partial result: %w", err)
	}

	return out, nil
}

func (r *ContentRunner) fetchKind(
	ctx context.Context,
	input *collection.RunInput,
	observationKind contract.ObservationKind,
	tab string,
) (*contract.Envelope, error) {
	subject := input.Subject()

	result, err := r.client.FetchContent(ctx, youtubejs.ContentRequest{
		ChannelID:               subject,
		Kind:                    tab,
		MaxResults:              maxResultsPerPage,
		MaxPages:                input.MaxPages(),
		MaxSuccessResponseBytes: input.MaxSuccessResponseBytes(),
	})
	if err != nil {
		return nil, fmt.Errorf("fetch content: %w", err)
	}

	if validateErr := validateContentIdentity(subject, result.Items); validateErr != nil {
		return nil, fmt.Errorf("validate content identity: %w", validateErr)
	}

	if result.MissingTab {
		//nolint:nilnil // 탭이 없으면 방출할 봉투가 없다는 뜻이라 오류가 아니다.
		return nil, nil
	}

	generation, err := input.Generation(observationKind)
	if err != nil {
		return nil, fmt.Errorf("generation: %w", err)
	}

	completeness, continuity, err := PaginationOf(&result.Pagination)
	if err != nil {
		return nil, fmt.Errorf("pagination of: %w", err)
	}

	videos, shorts := videoListPayload(subject, result.Items, maxResultsPerPage, &result.Pagination, tab == contentTabShorts)

	var payload any = videos

	if tab == contentTabShorts {
		payload = shorts
	}

	envelope, err := generationEnvelope(input, observationKind, generation, completeness, continuity, payload)
	if err != nil {
		return nil, err
	}

	return &envelope, nil
}
