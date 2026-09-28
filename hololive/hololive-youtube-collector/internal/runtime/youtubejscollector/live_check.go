package youtubejscollector

import (
	"context"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

type ChannelLiveCheckClient interface {
	FetchChannelLiveCheck(ctx context.Context, request youtubejs.ChannelLiveCheckRequest) (youtubejs.ChannelLiveCheckResult, error)
}

type VideoLiveCheckClient interface {
	FetchVideoLiveCheck(ctx context.Context, request youtubejs.VideoLiveCheckRequest) (youtubejs.VideoLiveCheckResult, error)
}

// ChannelLiveCheckRunner는 채널 /live 주소 해석 결과 하나를 자체 job 슬롯으로 확인합니다.
// 방송 탭 live_snapshot과 lease·재시도 슬롯을 공유하지 않으므로 방송 탭 실패가 확인의 진행을 막지 않습니다.
type ChannelLiveCheckRunner struct {
	client ChannelLiveCheckClient
}

func NewChannelLiveCheckRunner(client ChannelLiveCheckClient) *ChannelLiveCheckRunner {
	return &ChannelLiveCheckRunner{client: client}
}

func (r *ChannelLiveCheckRunner) JobID() sourceobservation.JobID {
	return sourceobservation.JobID{Provider: contract.ProviderYouTubeJS, Kind: "youtubejs_channel_live_check"}
}

func (r *ChannelLiveCheckRunner) Collect(ctx context.Context, input *collectutil.RunInput) (collectutil.CollectResult, error) {
	if r == nil || r.client == nil {
		return collectutil.CollectResult{}, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "youtube.js channel live check client is not configured")
	}

	if input == nil {
		return collectutil.CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is nil")
	}

	started := time.Now()

	allowed, err := input.Allows(contract.KindChannelLiveCheck, input.Spec().SubjectKey)
	if err != nil {
		return collectutil.CollectResult{}, fmt.Errorf("allows: %w", err)
	}

	if !allowed {
		return completeEmptyCollection(started)
	}

	envelope, err := channelLiveCheckEnvelope(ctx, r.client, input)
	if err != nil {
		return collectutil.CollectResult{}, fmt.Errorf("channel live check envelope: %w", err)
	}

	out, err := collectutil.CompleteFromEnvelopes([]contract.Envelope{envelope}, started)
	if err != nil {
		return out, fmt.Errorf("complete from envelopes: %w", err)
	}

	return out, nil
}

// VideoLiveCheckRunner는 target projection이 고른 영상 하나의 player 사실을 확인합니다.
// 결과는 수명 전이 근거일 뿐 absence slot이나 채널 부재 근거가 아닙니다.
type VideoLiveCheckRunner struct {
	client VideoLiveCheckClient
}

func NewVideoLiveCheckRunner(client VideoLiveCheckClient) *VideoLiveCheckRunner {
	return &VideoLiveCheckRunner{client: client}
}

func (r *VideoLiveCheckRunner) JobID() sourceobservation.JobID {
	return sourceobservation.JobID{Provider: contract.ProviderYouTubeJS, Kind: "youtubejs_video_live"}
}

func (r *VideoLiveCheckRunner) Collect(ctx context.Context, input *collectutil.RunInput) (collectutil.CollectResult, error) {
	if r == nil || r.client == nil {
		return collectutil.CollectResult{}, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "youtube.js video live check client is not configured")
	}

	if input == nil {
		return collectutil.CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is nil")
	}

	started := time.Now()

	allowed, err := input.Allows(contract.KindVideoLiveCheck, input.Spec().SubjectKey)
	if err != nil {
		return collectutil.CollectResult{}, fmt.Errorf("allows: %w", err)
	}

	if !allowed {
		return completeEmptyCollection(started)
	}

	envelope, err := r.videoLiveCheckEnvelope(ctx, input)
	if err != nil {
		return collectutil.CollectResult{}, fmt.Errorf("video live check envelope: %w", err)
	}

	out, err := collectutil.CompleteFromEnvelopes([]contract.Envelope{envelope}, started)
	if err != nil {
		return out, fmt.Errorf("complete from envelopes: %w", err)
	}

	return out, nil
}

func (r *VideoLiveCheckRunner) videoLiveCheckEnvelope(ctx context.Context, input *collectutil.RunInput) (contract.Envelope, error) {
	subject := input.Spec().SubjectKey

	result, err := r.client.FetchVideoLiveCheck(ctx, youtubejs.VideoLiveCheckRequest{
		VideoID:                 subject,
		MaxSuccessResponseBytes: input.MaxSuccessResponseBytes(),
	})
	if err == nil {
		err = validateLiveCheckSubject("video", subject, result.VideoID)
	}

	payload := failedVideoLiveCheck(subject)

	switch {
	case err == nil:
		payload = videoLiveCheckPayload(subject, &result)
	case !liveCheckRequestFailed(ctx, err):
		return contract.Envelope{}, fmt.Errorf("fetch video live check: %w", err)
	}

	completeness := contract.CompletenessPartial

	if payload.Availability == contract.VideoAvailabilityUnknown {
		completeness = contract.CompletenessUnknown
	}

	envelope, err := subjectEnvelope(input, contract.KindVideoLiveCheck, completeness, contract.ContinuityNotApplicable, payload)
	if err != nil && ctx.Err() == nil && collecterr.ClassOf(err) == collecterr.ClassDataContract {
		return subjectEnvelope(input, contract.KindVideoLiveCheck, contract.CompletenessUnknown, contract.ContinuityNotApplicable, failedVideoLiveCheck(subject))
	}

	return envelope, err
}

// channelLiveCheckEnvelope은 채널 /live 확인 envelope을 만듭니다. 요청 실패와 응답 계약 오류는 UNKNOWN으로 남깁니다.
func channelLiveCheckEnvelope(ctx context.Context, client ChannelLiveCheckClient, input *collectutil.RunInput) (contract.Envelope, error) {
	subject := input.Spec().SubjectKey

	result, err := client.FetchChannelLiveCheck(ctx, youtubejs.ChannelLiveCheckRequest{
		ChannelID:               subject,
		MaxSuccessResponseBytes: input.MaxSuccessResponseBytes(),
	})
	if err == nil {
		err = validateLiveCheckSubject("channel", subject, result.ChannelID)
	}

	payload := failedChannelLiveCheck(subject)

	switch {
	case err == nil:
		payload = channelLiveCheckPayload(subject, &result)
	case !liveCheckRequestFailed(ctx, err):
		return contract.Envelope{}, fmt.Errorf("fetch channel live check: %w", err)
	}

	completeness := contract.CompletenessComplete

	if payload.Outcome == contract.ChannelLiveCheckUnknown {
		completeness = contract.CompletenessUnknown
	}

	envelope, err := subjectEnvelope(input, contract.KindChannelLiveCheck, completeness, contract.ContinuityNotApplicable, payload)
	if err != nil && ctx.Err() == nil && collecterr.ClassOf(err) == collecterr.ClassDataContract {
		return subjectEnvelope(input, contract.KindChannelLiveCheck, contract.CompletenessUnknown, contract.ContinuityNotApplicable, failedChannelLiveCheck(subject))
	}

	return envelope, err
}

// liveCheckRequestFailed는 확인 요청의 실패를 해당 subject의 UNKNOWN(request_failed) 관측으로 남길지 판정합니다.
// 응답 계약 오류도 이전 음성·공개 불가 판정을 유지할 근거가 아니므로 UNKNOWN으로 대체한다.
// 수집 context 종료(취소·lease 기한)와 취소·대체·설정·불변식 오류는 반환해 publish fence를 유지한다.
func liveCheckRequestFailed(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}

	if collecterr.IsUnclassified(err) {
		return true
	}

	switch collecterr.ClassOf(err) {
	case collecterr.ClassTransient, collecterr.ClassTimeout, collecterr.ClassCooldown,
		collecterr.ClassResourceLimit, collecterr.ClassDataContract, collecterr.ClassProtocol:
		return true
	case collecterr.ClassCanceled, collecterr.ClassConfiguration,
		collecterr.ClassSuperseded, collecterr.ClassInternal:
		return false
	default:
		return false
	}
}

// validateLiveCheckSubject는 helper가 돌려준 subject가 요청 subject와 같은지 확인합니다.
// 판정이 UNKNOWN이어도 다른 subject의 응답은 이 subject의 관측이 아닙니다.
func validateLiveCheckSubject(field, requested, returned string) error {
	if returned != requested {
		return collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract,
			"youtube.js "+field+" live check response identity does not match request")
	}

	return nil
}

func channelLiveCheckPayload(subject string, result *youtubejs.ChannelLiveCheckResult) contract.ChannelLiveCheckV1 {
	return contract.ChannelLiveCheckV1{
		ChannelID:                subject,
		Outcome:                  result.Outcome,
		SelectedVideoID:          result.SelectedVideoID,
		ChannelIdentityConfirmed: result.ChannelIdentityConfirmed,
		UnknownReason:            result.UnknownReason,
		Coverage:                 contract.ChannelLiveCheckCoverageV1{ChannelID: subject},
	}
}

// failedChannelLiveCheck는 응답을 얻지 못한 확인을 identity 미확인 UNKNOWN으로 기록합니다.
func failedChannelLiveCheck(subject string) contract.ChannelLiveCheckV1 {
	return contract.ChannelLiveCheckV1{
		ChannelID:     subject,
		Outcome:       contract.ChannelLiveCheckUnknown,
		UnknownReason: contract.LiveCheckReasonRequestFailed,
		Coverage:      contract.ChannelLiveCheckCoverageV1{ChannelID: subject},
	}
}

func videoLiveCheckPayload(subject string, result *youtubejs.VideoLiveCheckResult) contract.VideoLiveCheckV1 {
	return contract.VideoLiveCheckV1{
		VideoID:                 subject,
		ChannelID:               result.ChannelID,
		IdentityConfirmed:       result.IdentityConfirmed,
		IsLive:                  result.IsLive,
		IsLiveNow:               result.IsLiveNow,
		IsUpcoming:              result.IsUpcoming,
		IsLiveContent:           result.IsLiveContent,
		IsPrivate:               result.IsPrivate,
		HasLiveBroadcastDetails: result.HasLiveBroadcastDetails,
		StartedAt:               result.StartedAt,
		EndedAt:                 result.EndedAt,
		Availability:            result.Availability,
		Method:                  result.Method,
		UnknownReason:           result.UnknownReason,
		Coverage:                contract.VideoLiveCheckCoverageV1{VideoID: subject},
	}
}

// failedVideoLiveCheck는 응답을 얻지 못한 확인을 사실 없는 UNKNOWN으로 기록합니다. 채널을 추정해 채우지 않습니다.
func failedVideoLiveCheck(subject string) contract.VideoLiveCheckV1 {
	return contract.VideoLiveCheckV1{
		VideoID:       subject,
		Availability:  contract.VideoAvailabilityUnknown,
		Method:        contract.VideoAvailabilityMethodUnknown,
		UnknownReason: contract.LiveCheckReasonRequestFailed,
		Coverage:      contract.VideoLiveCheckCoverageV1{VideoID: subject},
	}
}
