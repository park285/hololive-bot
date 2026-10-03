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

const (
	channelKindLive     = "live"
	channelKindMetadata = "metadata"
)

type ChannelClient interface {
	FetchChannel(ctx context.Context, request youtubejs.ChannelRequest) (youtubejs.ChannelResult, error)
}

// ChannelRunner는 방송 일정 조회 없이 채널 프로필·사진만 수집합니다.
type ChannelRunner struct {
	client ChannelClient
}

// NewChannelMetadataRunner는 방송 일정 조회 없이 채널 프로필·사진만 수집합니다.
func NewChannelMetadataRunner(client ChannelClient) *ChannelRunner {
	return &ChannelRunner{client: client}
}

func (r *ChannelRunner) JobID() collection.JobID {
	return collection.JobID{Provider: contract.ProviderYouTubeJS, Kind: "youtubejs_channel_" + channelKindMetadata}
}

// Collect는 수집 범위에 맞는 observation과 checkpoint를 구성하며 실제 DB 저장은 publisher에 맡깁니다.
func (r *ChannelRunner) Collect(ctx context.Context, input *collection.RunInput) (collection.CollectResult, error) {
	if r == nil || r.client == nil {
		return collection.CollectResult{}, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "youtube.js channel client is not configured")
	}

	if input == nil {
		return collection.CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is nil")
	}

	started := time.Now()

	enabled, err := enabledChannelKinds(input, input.Job().Emissions())
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("enabled channel kinds: %w", err)
	}

	if !enabled[contract.KindChannelProfile] && !enabled[contract.KindChannelPhoto] {
		out, completeErr := completeEmptyCollection(started)

		return out, completeErr
	}

	result, completeness, continuity, err := fetchChannelPage(ctx, r.client, input, channelKindMetadata)
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("fetch channel page: %w", err)
	}

	envelopes, err := channelMetadataEnvelopes(input, &result, enabled, completeness, continuity)
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("channel envelopes: %w", err)
	}

	out, err := collection.CompleteFromEnvelopes(envelopes, started)
	if err != nil {
		return out, fmt.Errorf("complete from envelopes: %w", err)
	}

	return out, nil
}

func completeEmptyCollection(started time.Time) (collection.CollectResult, error) {
	out, err := collection.CompleteFromEnvelopes(nil, started)
	if err != nil {
		return out, fmt.Errorf("complete from envelopes: %w", err)
	}

	return out, nil
}

func enabledChannelKinds(input *collection.RunInput, kinds []contract.ObservationKind) (map[contract.ObservationKind]bool, error) {
	enabled := make(map[contract.ObservationKind]bool, len(kinds))

	for _, kind := range kinds {
		allowed, err := input.Allows(kind, input.Subject())
		if err != nil {
			return nil, fmt.Errorf("allows: %w", err)
		}

		enabled[kind] = allowed
	}

	return enabled, nil
}

func fetchChannelPage(
	ctx context.Context,
	client ChannelClient,
	input *collection.RunInput,
	kind string,
) (youtubejs.ChannelResult, contract.Completeness, contract.Continuity, error) {
	result, err := client.FetchChannel(ctx, youtubejs.ChannelRequest{
		ChannelID:               input.Subject(),
		Kind:                    kind,
		MaxPages:                input.MaxPages(),
		MaxSuccessResponseBytes: input.MaxSuccessResponseBytes(),
	})
	if err != nil {
		return youtubejs.ChannelResult{}, "", "", fmt.Errorf("fetch channel: %w", err)
	}

	if validateErr := validateLiveIdentity(input.Subject(), result.LiveSessions); validateErr != nil {
		return youtubejs.ChannelResult{}, "", "", fmt.Errorf("validate live identity: %w", validateErr)
	}

	if validateErr := validateUnavailableLiveSessions(input.Subject(), kind, &result); validateErr != nil {
		return youtubejs.ChannelResult{}, "", "", fmt.Errorf("validate unavailable live sessions: %w", validateErr)
	}

	completeness, continuity, err := PaginationOf(&result.Pagination)
	if err != nil {
		return youtubejs.ChannelResult{}, "", "", fmt.Errorf("pagination of: %w", err)
	}

	if len(result.UnavailableLiveSessions) > 0 {
		// 목록 조회가 끝나도 시각이 가려진 영상은 부재 증거가 아닙니다. 다음 poll은 정상 진행합니다.
		completeness = contract.CompletenessPartial
	}

	return result, completeness, continuity, nil
}

func channelMetadataEnvelopes(
	input *collection.RunInput,
	result *youtubejs.ChannelResult,
	enabled map[contract.ObservationKind]bool,
	completeness contract.Completeness,
	continuity contract.Continuity,
) ([]contract.Envelope, error) {
	subject := input.Subject()
	envelopes := make([]contract.Envelope, 0, 2)

	profile, ok := channelProfilePayload(subject, result.Profile)
	if err := appendBuiltEnvelope(input, contract.KindChannelProfile, enabled, completeness, continuity, profile, ok, &envelopes); err != nil {
		return nil, fmt.Errorf("append channel profile: %w", err)
	}

	photo, ok := channelPhotoPayload(subject, result.Photo)
	if err := appendBuiltEnvelope(input, contract.KindChannelPhoto, enabled, completeness, continuity, photo, ok, &envelopes); err != nil {
		return nil, fmt.Errorf("append channel photo: %w", err)
	}

	return envelopes, nil
}

func appendBuiltEnvelope(
	input *collection.RunInput,
	kind contract.ObservationKind,
	enabled map[contract.ObservationKind]bool,
	completeness contract.Completeness,
	continuity contract.Continuity,
	payload any,
	ok bool,
	envelopes *[]contract.Envelope,
) error {
	if !ok || !enabled[kind] {
		return nil
	}

	envelope, err := subjectEnvelope(input, kind, completeness, continuity, payload)
	if err != nil {
		return fmt.Errorf("envelope: %w", err)
	}

	*envelopes = append(*envelopes, envelope)

	return nil
}

// subjectEnvelope은 lease가 배정한 수집 슬롯과 요청 subject로 envelope 하나를 만듭니다.
// 공유 계약이 payload를 거부하면 원천 응답과 계약의 불일치로 분류합니다.
func subjectEnvelope(
	input *collection.RunInput,
	kind contract.ObservationKind,
	completeness contract.Completeness,
	continuity contract.Continuity,
	payload any,
) (contract.Envelope, error) {
	generation, err := input.Generation(kind)
	if err != nil {
		return contract.Envelope{}, fmt.Errorf("generation: %w", err)
	}

	return generationEnvelope(input, kind, generation, completeness, continuity, payload)
}

// generationEnvelope은 호출자가 먼저 확인한 generation으로 subject envelope 하나를 만듭니다.
// 세대 오류를 응답 pagination 검증보다 먼저 보고해야 하는 수집기가 사용합니다.
func generationEnvelope(
	input *collection.RunInput,
	kind contract.ObservationKind,
	generation int64,
	completeness contract.Completeness,
	continuity contract.Continuity,
	payload any,
) (contract.Envelope, error) {
	lease := input.Lease()

	envelope, err := collection.Envelope(
		contract.ProviderYouTubeJS,
		kind,
		input.Subject(),
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
