package youtubejscollector

import (
	"context"
	"encoding/json/jsontext"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

type ContentClient interface {
	FetchContent(ctx context.Context, request youtubejs.ContentRequest) (youtubejs.ContentResult, error)
	FetchVideoLiveCheck(ctx context.Context, request youtubejs.VideoLiveCheckRequest) (youtubejs.VideoLiveCheckResult, error)
}

type ContentRunner struct {
	client             ContentClient
	cursors            ContentCursorReader
	publicationReserve time.Duration
}

type contentKind struct {
	kind contract.ObservationKind
	tab  string
}

type contentObservation struct {
	envelope contract.Envelope
	cursor   jsontext.Value
}

type contentListFetch struct {
	result       youtubejs.ContentResult
	generation   int64
	completeness contract.Completeness
	continuity   contract.Continuity
}

const (
	contentTabVideos = "videos"
	contentTabShorts = "shorts"
)

const ContentJobKind collection.JobKind = "youtubejs_content"

var contentKinds = []contentKind{
	{kind: contract.KindVideoList, tab: contentTabVideos},
	{kind: contract.KindShortsList, tab: contentTabShorts},
}

func NewContentRunner(client ContentClient, cursors ContentCursorReader, publicationReserve time.Duration) *ContentRunner {
	return &ContentRunner{client: client, cursors: cursors, publicationReserve: max(publicationReserve, 0)}
}

func (r *ContentRunner) JobID() collection.JobID {
	return collection.JobID{Provider: contract.ProviderYouTubeJS, Kind: ContentJobKind}
}

func (r *ContentRunner) Collect(ctx context.Context, input *collection.RunInput) (collection.CollectResult, error) {
	if r == nil || r.client == nil || r.cursors == nil {
		return collection.CollectResult{}, collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "youtube.js content client is not configured")
	}

	if input == nil {
		return collection.CollectResult{}, collecterr.New(contract.ErrorInternalInvariant, contract.ClassInternal, "collection run input is nil")
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
	var (
		videos videoListFetch
		shorts *contentObservation
	)

	for _, item := range contentKinds {
		if item.kind == contract.KindVideoList {
			fetched, err := r.collectVideoList(ctx, input)
			if err != nil {
				return partialContentResultForError(ctx, nil, started, item.kind, err)
			}

			videos = fetched

			continue
		}

		fetched, err := r.collectKind(ctx, input, item)
		if err == nil && fetched != nil {
			var observation contentObservation

			observation, err = shortsObservation(input, fetched)
			shorts = &observation
		}

		if err != nil {
			if ctx.Err() != nil || !collection.PartialFailureClassAllowed(collecterr.ClassOf(err)) {
				return partialContentResultForError(ctx, nil, started, item.kind, err)
			}

			observations, videoErr := r.videoListObservations(ctx, input, videos)
			if videoErr != nil {
				return collection.CollectResult{}, videoErr
			}

			return partialContentResultForError(ctx, observations, started, item.kind, err)
		}
	}

	observations, err := r.videoListObservations(ctx, input, videos)
	if err != nil {
		return collection.CollectResult{}, err
	}

	if shorts != nil {
		observations = append(observations, *shorts)
	}

	output, err := contentOutput(observations, started)
	if err != nil {
		return collection.CollectResult{}, err
	}

	return collection.NewCompleteResult(output), nil
}

type videoListFetch struct {
	list  *contentListFetch
	prior storedPublicationCursor
}

func (r *ContentRunner) collectVideoList(ctx context.Context, input *collection.RunInput) (videoListFetch, error) {
	subject := input.Subject()

	allowed, err := input.Allows(contract.KindVideoList, subject)
	if err != nil {
		return videoListFetch{}, fmt.Errorf("allows: %w", err)
	}

	if !allowed {
		return videoListFetch{}, nil
	}

	prior, err := r.loadPublicationCursor(ctx, subject)
	if err != nil {
		return videoListFetch{}, fmt.Errorf("load publication cursor: %w", err)
	}

	if prior.scheduledFor.Equal(input.Lease().ScheduledFor) {
		return videoListFetch{}, nil
	}

	fetched, err := r.fetchKind(ctx, input, contract.KindVideoList, contentTabVideos)
	if err != nil {
		return videoListFetch{}, fmt.Errorf("fetch kind: %w", err)
	}

	return videoListFetch{list: fetched, prior: prior}, nil
}

func (r *ContentRunner) videoListObservations(
	ctx context.Context,
	input *collection.RunInput,
	videos videoListFetch,
) ([]contentObservation, error) {
	observations := make([]contentObservation, 0, len(contentKinds))

	if videos.list == nil {
		return observations, nil
	}

	observation, err := r.videoListObservation(ctx, input, videos.list, videos.prior)
	if err != nil {
		return nil, fmt.Errorf("video list observation: %w", err)
	}

	return append(observations, observation), nil
}

func contentOutput(observations []contentObservation, started time.Time) (collection.RunOutput, error) {
	envelopes := make([]contract.Envelope, len(observations))
	cursors := make([]jsontext.Value, len(observations))

	for i := range observations {
		envelopes[i] = observations[i].envelope
		cursors[i] = observations[i].cursor
	}

	output, err := collection.NewRunOutputWithCursors(envelopes, cursors, collection.ClampLatency(started))
	if err != nil {
		return output, fmt.Errorf("run output: %w", err)
	}

	return output, nil
}

func partialContentResultForError(ctx context.Context, observations []contentObservation, started time.Time, kind contract.ObservationKind, cause error) (collection.CollectResult, error) {
	out, err := partialContentResult(ctx, observations, started, kind, cause)
	if err != nil {
		return out, fmt.Errorf("partial content result: %w", err)
	}

	return out, nil
}

func (r *ContentRunner) collectKind(ctx context.Context, input *collection.RunInput, item contentKind) (*contentListFetch, error) {
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
	observations []contentObservation,
	started time.Time,
	kind contract.ObservationKind,
	err error,
) (collection.CollectResult, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return collection.CollectResult{}, fmt.Errorf("collect content: %w", ctxErr)
	}

	if len(observations) == 0 || !collection.PartialFailureClassAllowed(collecterr.ClassOf(err)) {
		return collection.CollectResult{}, err
	}

	output, buildErr := contentOutput(observations, started)
	if buildErr != nil {
		return collection.CollectResult{}, fmt.Errorf("content output: %w", buildErr)
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
) (*contentListFetch, error) {
	subject := input.Subject()

	generation, err := input.Generation(observationKind)
	if err != nil {
		return nil, fmt.Errorf("generation: %w", err)
	}

	if observationKind == contract.KindVideoList && generation != contract.VideoListPublicationContractGeneration {
		return nil, collecterr.New(contract.ErrorConfiguration, contract.ClassConfiguration, "video list collector requires publication contract generation 2")
	}

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

	completeness, continuity, err := PaginationOf(&result.Pagination)
	if err != nil {
		return nil, fmt.Errorf("pagination of: %w", err)
	}

	return &contentListFetch{result: result, generation: generation, completeness: completeness, continuity: continuity}, nil
}

func shortsObservation(input *collection.RunInput, fetched *contentListFetch) (contentObservation, error) {
	payload := shortsListPayload(input.Subject(), fetched.result.Items, maxResultsPerPage, &fetched.result.Pagination)

	envelope, err := generationEnvelope(input, contract.KindShortsList, fetched.generation, fetched.completeness, fetched.continuity, payload)
	if err != nil {
		return contentObservation{}, err
	}

	return contentObservation{envelope: envelope}, nil
}

func (r *ContentRunner) videoListObservation(
	ctx context.Context,
	input *collection.RunInput,
	fetched *contentListFetch,
	prior storedPublicationCursor,
) (contentObservation, error) {
	subject := input.Subject()

	enrichCtx, cancel := r.publicationContext(ctx)
	publications, cursor, err := r.enrichPublications(enrichCtx, ctx, subject, fetched.result.Items, prior, input.MaxSuccessResponseBytes())

	cancel()

	if err != nil {
		return contentObservation{}, fmt.Errorf("enrich publications: %w", err)
	}

	cursor.ScheduledFor = input.Lease().ScheduledFor.UTC()

	payload := videoListPayload(subject, fetched.result.Items, publications, maxResultsPerPage, &fetched.result.Pagination)

	envelope, err := generationEnvelope(input, contract.KindVideoList, fetched.generation, fetched.completeness, fetched.continuity, payload)
	if err != nil {
		return contentObservation{}, err
	}

	rawCursor, err := marshalPublicationCursor(cursor)
	if err != nil {
		return contentObservation{}, collecterr.Wrap(contract.ErrorInternalInvariant, contract.ClassInternal, err)
	}

	return contentObservation{envelope: envelope, cursor: rawCursor}, nil
}

func (r *ContentRunner) publicationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return context.WithCancel(ctx)
	}

	return context.WithDeadline(ctx, deadline.Add(-r.publicationReserve))
}
