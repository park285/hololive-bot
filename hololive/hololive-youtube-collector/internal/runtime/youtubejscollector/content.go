package youtubejscollector

import (
	"context"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

// ContentClient는 목록 RPC와, video_list 공개 근거를 얻는 영상별 확인 RPC를 제공합니다. 두 RPC 모두 호출마다 limiter를 기다립니다.
type ContentClient interface {
	FetchContent(ctx context.Context, request youtubejs.ContentRequest) (youtubejs.ContentResult, error)
	FetchVideoLiveCheck(ctx context.Context, request youtubejs.VideoLiveCheckRequest) (youtubejs.VideoLiveCheckResult, error)
}

type ContentRunner struct {
	client     ContentClient
	cursors    ContentCursorReader
	maxResults int
	// publicationReserve는 공개 근거 조회가 쓰지 않고 남겨 두는 수집 기한입니다. 실행 profile의 RPC 외 작업 여유(overhead)이며,
	// 근거 조회가 수집 기한을 끝까지 써서 이미 받은 목록·근거까지 버려지지 않게 합니다.
	publicationReserve time.Duration
}

type contentKind struct {
	kind contract.ObservationKind
	tab  string
}

// contentObservation은 kind 하나의 관측과 그 checkpoint입니다. 공개 근거 cursor는 video_list checkpoint만 싣습니다.
type contentObservation struct {
	envelope   contract.Envelope
	checkpoint sourceobservation.CheckpointEntry
}

// contentListFetch는 목록 RPC 결과와 envelope에 필요한 계약 세대·완결성입니다.
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

var contentKinds = []contentKind{
	{kind: contract.KindVideoList, tab: contentTabVideos},
	{kind: contract.KindShortsList, tab: contentTabShorts},
}

func NewContentRunner(client ContentClient, cursors ContentCursorReader, maxResults int, publicationReserve time.Duration) *ContentRunner {
	return &ContentRunner{
		client: client, cursors: cursors, maxResults: collectutil.MaxResults(maxResults), publicationReserve: max(publicationReserve, 0),
	}
}

func (r *ContentRunner) JobID() sourceobservation.JobID {
	return sourceobservation.JobID{Provider: contract.ProviderYouTubeJS, Kind: "youtubejs_content"}
}

func (r *ContentRunner) Collect(ctx context.Context, input *collectutil.RunInput) (collectutil.CollectResult, error) {
	if r == nil || r.client == nil || r.cursors == nil {
		return collectutil.CollectResult{}, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "youtube.js content client is not configured")
	}

	if input == nil {
		return collectutil.CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is nil")
	}

	out, err := r.collectAllowedKinds(ctx, input, time.Now())
	if err != nil {
		return out, fmt.Errorf("collect allowed kinds: %w", err)
	}

	return out, nil
}

// collectAllowedKinds는 두 목록 RPC를 먼저 마친 뒤 video_list 공개 근거를 조회합니다. 근거 조회가 목록 RPC의 수집 기한을
// 먼저 쓰지 않게 하려는 순서입니다. 부분 결과는 기존 정책대로 video_list 관측만 싣습니다. 근거 조회 단계의 오류는
// 근거 조회 실패로 강등할 수 없는 오류뿐이라 job 전체를 실패시킵니다.
//
// 같은 slot의 video_list가 이미 수락됐으면(DEFERRED 재시도) 목록과 근거를 다시 조회하지 않습니다. 같은 slot은 같은 관측
// identity이므로 다시 만든 payload가 달라지면 collision이 되고 cursor 전진도 저장되지 않기 때문입니다.
func (r *ContentRunner) collectAllowedKinds(
	ctx context.Context,
	input *collectutil.RunInput,
	started time.Time,
) (collectutil.CollectResult, error) {
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

			observation, err = shortsObservation(input, fetched, r.maxResults)
			shorts = &observation
		}

		if err != nil {
			// 부분 결과로 발행할 수 없는 실패면 근거 조회 RPC를 쓰지 않고 바로 실패합니다.
			if ctx.Err() != nil || !contentPartialFailureAllowed(collecterr.ClassOf(err)) {
				return partialContentResultForError(ctx, nil, started, item.kind, err)
			}

			observations, videoErr := r.videoListObservations(ctx, input, videos)
			if videoErr != nil {
				return collectutil.CollectResult{}, videoErr
			}

			return partialContentResultForError(ctx, observations, started, item.kind, err)
		}
	}

	observations, err := r.videoListObservations(ctx, input, videos)
	if err != nil {
		return collectutil.CollectResult{}, err
	}

	if shorts != nil {
		observations = append(observations, *shorts)
	}

	output, err := contentOutput(observations, started)
	if err != nil {
		return collectutil.CollectResult{}, err
	}

	out, err := collectutil.NewCompleteResult(output)
	if err != nil {
		return out, fmt.Errorf("complete result: %w", err)
	}

	return out, nil
}

// videoListFetch는 이 slot에서 받은 video_list 목록과 직전에 수락된 공개 근거 cursor입니다.
// List가 nil이면 video_list가 수집 대상이 아니거나, 이 slot에서 이미 수락됐거나, 탭이 없어 방출할 관측이 없습니다.
type videoListFetch struct {
	list  *contentListFetch
	prior storedPublicationCursor
}

// collectVideoList는 video_list가 수집 대상이고 이 slot에서 아직 수락되지 않았을 때만 목록 RPC를 보냅니다.
// 직전 cursor의 slot이 현재 lease slot과 같다는 것은 같은 publish tx로 이 slot의 video_list가 수락됐다는 뜻입니다.
func (r *ContentRunner) collectVideoList(ctx context.Context, input *collectutil.RunInput) (videoListFetch, error) {
	subject := input.Spec().SubjectKey

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

// videoListObservations는 받은 video_list 목록이 있으면 근거를 붙인 관측 하나를, 없으면 빈 목록을 반환합니다.
func (r *ContentRunner) videoListObservations(
	ctx context.Context,
	input *collectutil.RunInput,
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

func contentOutput(observations []contentObservation, started time.Time) (collectutil.RunOutput, error) {
	envelopes := make([]contract.Envelope, len(observations))
	checkpoints := make([]sourceobservation.CheckpointEntry, len(observations))

	for i := range observations {
		envelopes[i] = observations[i].envelope
		checkpoints[i] = observations[i].checkpoint
	}

	output, err := collectutil.NewRunOutput(envelopes, checkpoints, collectutil.ClampLatency(started))
	if err != nil {
		return output, fmt.Errorf("run output: %w", err)
	}

	return output, nil
}

func partialContentResultForError(ctx context.Context, observations []contentObservation, started time.Time, kind contract.ObservationKind, cause error) (collectutil.CollectResult, error) {
	out, err := partialContentResult(ctx, observations, started, kind, cause)
	if err != nil {
		return out, fmt.Errorf("partial content result: %w", err)
	}

	return out, nil
}

func (r *ContentRunner) collectKind(ctx context.Context, input *collectutil.RunInput, item contentKind) (*contentListFetch, error) {
	allowed, err := input.Allows(item.kind, input.Spec().SubjectKey)
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
) (collectutil.CollectResult, error) {
	if ctx.Err() != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return collectutil.CollectResult{}, fmt.Errorf("collect content: %w", ctxErr)
		}

		return collectutil.CollectResult{}, nil
	}

	if len(observations) == 0 || !contentPartialFailureAllowed(collecterr.ClassOf(err)) {
		return collectutil.CollectResult{}, err
	}

	output, buildErr := contentOutput(observations, started)
	if buildErr != nil {
		return collectutil.CollectResult{}, fmt.Errorf("content output: %w", buildErr)
	}

	out, err := collectutil.NewPartialResult(output, collecterr.Normalize(err), kind)
	if err != nil {
		return out, fmt.Errorf("partial result: %w", err)
	}

	return out, nil
}

func (r *ContentRunner) fetchKind(
	ctx context.Context,
	input *collectutil.RunInput,
	observationKind contract.ObservationKind,
	tab string,
) (*contentListFetch, error) {
	spec := input.Spec()

	generation, err := input.Generation(observationKind)
	if err != nil {
		return nil, fmt.Errorf("generation: %w", err)
	}

	if observationKind == contract.KindVideoList && generation != contract.VideoListPublicationContractGeneration {
		return nil, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "video list collector requires publication contract generation 2")
	}

	result, err := r.client.FetchContent(ctx, youtubejs.ContentRequest{
		ChannelID:               spec.SubjectKey,
		Kind:                    tab,
		MaxResults:              r.maxResults,
		MaxPages:                input.MaxPages(),
		MaxSuccessResponseBytes: input.MaxSuccessResponseBytes(),
	})
	if err != nil {
		return nil, fmt.Errorf("fetch content: %w", err)
	}

	if validateErr := validateContentIdentity(spec.SubjectKey, result.Items); validateErr != nil {
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

func shortsObservation(input *collectutil.RunInput, fetched *contentListFetch, maxResults int) (contentObservation, error) {
	payload := shortsListPayload(input.Spec().SubjectKey, fetched.result.Items, maxResults, &fetched.result.Pagination)

	envelope, err := contentEnvelope(input, contract.KindShortsList, fetched.generation, fetched.completeness, fetched.continuity, payload)
	if err != nil {
		return contentObservation{}, err
	}

	return contentObservation{envelope: envelope, checkpoint: collectutil.Checkpoint(&envelope)}, nil
}

// videoListObservation은 목록 항목에 영상별 공개 근거를 붙이고, 근거 조회 이력을 같은 checkpoint cursor로 남깁니다.
// 근거 조회는 수집 기한에서 publicationReserve를 남긴 하위 기한 안에서만 하므로, 근거 조회가 끊겨도 그때까지 받은 근거와
// 목록 관측을 발행합니다. 근거 조회 실패는 해당 항목을 근거 미수집으로 남길 뿐 목록 관측 자체를 버리지 않습니다.
func (r *ContentRunner) videoListObservation(
	ctx context.Context,
	input *collectutil.RunInput,
	fetched *contentListFetch,
	prior storedPublicationCursor,
) (contentObservation, error) {
	subject := input.Spec().SubjectKey

	enrichCtx, cancel := r.publicationContext(ctx)
	publications, cursor, err := r.enrichPublications(enrichCtx, ctx, subject, fetched.result.Items, prior, input.MaxSuccessResponseBytes())

	cancel()

	if err != nil {
		return contentObservation{}, fmt.Errorf("enrich publications: %w", err)
	}

	cursor.ScheduledFor = input.Lease().ScheduledFor.UTC()

	payload := videoListPayload(subject, fetched.result.Items, publications, r.maxResults, &fetched.result.Pagination)

	envelope, err := contentEnvelope(input, contract.KindVideoList, fetched.generation, fetched.completeness, fetched.continuity, payload)
	if err != nil {
		return contentObservation{}, err
	}

	rawCursor, err := marshalPublicationCursor(cursor)
	if err != nil {
		return contentObservation{}, collecterr.Wrap(collecterr.Internal, collecterr.ClassInternal, err)
	}

	checkpoint := collectutil.Checkpoint(&envelope)

	checkpoint.Cursor = rawCursor

	return contentObservation{envelope: envelope, checkpoint: checkpoint}, nil
}

// publicationContext는 수집 기한에서 publicationReserve만큼 앞당긴 근거 조회 기한입니다. 수집 기한이 없으면 부모 문맥을 그대로 씁니다.
func (r *ContentRunner) publicationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return context.WithCancel(ctx)
	}

	return context.WithDeadline(ctx, deadline.Add(-r.publicationReserve))
}

func contentEnvelope(
	input *collectutil.RunInput,
	observationKind contract.ObservationKind,
	generation int64,
	completeness contract.Completeness,
	continuity contract.Continuity,
	payload any,
) (contract.Envelope, error) {
	lease := input.Lease()

	envelope, err := collectutil.Envelope(
		contract.ProviderYouTubeJS,
		observationKind,
		input.Spec().SubjectKey,
		generation,
		&lease,
		completeness,
		continuity,
		payload,
	)
	if err != nil {
		return contract.Envelope{}, collecterr.Wrap(collecterr.ParserDrift, collecterr.ClassDataContract, err)
	}

	return envelope, nil
}
