package youtubejscollector

import (
	"context"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collection"
)

// ChannelLiveRunner는 방송 탭 live_snapshot만 수집합니다. 채널 /live 확인은 별도 job 슬롯이 담당하므로
// 방송 탭 실패는 이 job의 슬롯만 재시도하고 확인 관측의 진행을 막지 않습니다.
type ChannelLiveRunner struct {
	client ChannelClient
}

func NewChannelLiveRunner(client ChannelClient) *ChannelLiveRunner {
	return &ChannelLiveRunner{client: client}
}

func (r *ChannelLiveRunner) JobID() collection.JobID {
	return collection.JobID{Provider: contract.ProviderYouTubeJS, Kind: "youtubejs_channel_" + channelKindLive}
}

func (r *ChannelLiveRunner) Collect(ctx context.Context, input *collection.RunInput) (collection.CollectResult, error) {
	if r == nil || r.client == nil {
		return collection.CollectResult{}, collecterr.New(collecterr.Configuration, collecterr.ClassConfiguration, "youtube.js channel client is not configured")
	}

	if input == nil {
		return collection.CollectResult{}, collecterr.New(collecterr.Internal, collecterr.ClassInternal, "collection run input is nil")
	}

	started := time.Now()

	allowed, err := input.Allows(contract.KindLiveSnapshot, input.Subject())
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("allows: %w", err)
	}

	if !allowed {
		return completeEmptyCollection(started)
	}

	snapshot, err := r.liveSnapshotEnvelope(ctx, input)
	if err != nil {
		return collection.CollectResult{}, fmt.Errorf("live snapshot: %w", err)
	}

	// 방송 탭이 없으면 snapshot 없이 성공합니다. 실패한 목록 조회는 빈 snapshot으로 바꾸지 않습니다.
	var envelopes []contract.Envelope

	if snapshot != nil {
		envelopes = []contract.Envelope{*snapshot}
	}

	out, err := collection.CompleteFromEnvelopes(envelopes, started)
	if err != nil {
		return out, fmt.Errorf("complete from envelopes: %w", err)
	}

	return out, nil
}

func (r *ChannelLiveRunner) liveSnapshotEnvelope(ctx context.Context, input *collection.RunInput) (*contract.Envelope, error) {
	result, completeness, continuity, err := fetchChannelPage(ctx, r.client, input, channelKindLive)
	if err != nil {
		return nil, fmt.Errorf("fetch channel page: %w", err)
	}

	if validateErr := validateLiveSchedules(result.LiveSessions); validateErr != nil {
		return nil, fmt.Errorf("validate live schedules: %w", validateErr)
	}

	if result.MissingTab {
		//nolint:nilnil // 방송 탭이 없으면 방출할 snapshot이 없다는 뜻이라 오류가 아니다.
		return nil, nil
	}

	// 새 collector는 generation 3의 실제 조회 증명만 발행한다.
	if generationErr := input.RequireLiveSnapshotMetadataGeneration(); generationErr != nil {
		return nil, fmt.Errorf("require live snapshot metadata generation: %w", generationErr)
	}

	if result.LiveQuery == nil || result.LiveQuery.ChannelID != input.Subject() ||
		result.LiveQuery.PageCount != result.PageCount || result.LiveQuery.Exhausted != result.Exhausted ||
		result.LiveQuery.AccessRestricted != (len(result.UnavailableLiveSessions) > 0) {
		return nil, collecterr.New(collecterr.ParserDrift, collecterr.ClassDataContract, "live query proof does not match helper pagination and restrictions")
	}

	payload := liveSnapshotPayload(input.Subject(), result.LiveSessions, result.LiveQuery)

	envelope, err := subjectEnvelope(input, contract.KindLiveSnapshot, completeness, continuity, payload)
	if err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}

	return &envelope, nil
}
