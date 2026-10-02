package youtubedispatch

import (
	"context"
	"sync"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type batchClaimKey struct {
	kind         domain.OutboxKind
	postID       string
	authorizedAt time.Time
}

type batchClaimUse struct {
	token    dispatchstate.ClaimToken
	selected int
	// 각 소비자는 준비·전송의 서로 배타적인 확정 실패 분기에서 한 번만 해제를 요청한다.
	released int
}

// batchClaimResolver는 모든 방의 처리가 끝날 때까지 공유 claim의 완료 증명을 보존한다.
// 확정 실패로 해제를 요청하지 않은 소비자(성공, 결과 불명, 미정산)가 있으면 해제하지 않는다.
type batchClaimResolver struct {
	ClaimResolver

	mu   sync.Mutex
	uses map[batchClaimKey]batchClaimUse
}

func newBatchClaimResolver(resolver ClaimResolver) *batchClaimResolver {
	return &batchClaimResolver{ClaimResolver: resolver, uses: make(map[batchClaimKey]batchClaimUse)}
}

func (b *batchClaimResolver) selectClaimedDeliveries(ctx context.Context, rows []domain.YouTubeNotificationDelivery, outboxes []domain.YouTubeNotificationOutbox, cache *claimDecisionCache) deliveryClaimSelection {
	selection := b.ClaimResolver.selectClaimedDeliveries(ctx, rows, outboxes, cache)
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, token := range selection.claimTokens {
		key := batchClaimKey{kind: token.Kind, postID: token.PostID, authorizedAt: token.AuthorizedAt}
		use := b.uses[key]

		if !token.Reused {
			use.token = token
		}

		use.selected++

		b.uses[key] = use
	}

	return selection
}

func (b *batchClaimResolver) releaseDeliveryClaims(_ context.Context, tokens []dispatchstate.ClaimToken) error {
	b.queueRelease(tokens)

	return nil
}

func (b *batchClaimResolver) queueRelease(tokens []dispatchstate.ClaimToken) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, token := range tokens {
		key := batchClaimKey{kind: token.Kind, postID: token.PostID, authorizedAt: token.AuthorizedAt}
		use := b.uses[key]
		use.released++

		b.uses[key] = use
	}
}

func (b *batchClaimResolver) releaseDeliveryClaimsWithWarning(_ context.Context, tokens []dispatchstate.ClaimToken, _ string, _ ...any) {
	// 해제 요청은 배치 종료 때 실행하므로 이 시점에는 DB 오류가 발생하지 않는다.
	b.queueRelease(tokens)
}

func (b *batchClaimResolver) releaseFinished(ctx context.Context) {
	b.mu.Lock()

	var tokens []dispatchstate.ClaimToken

	for _, use := range b.uses {
		if use.selected > 0 && use.selected == use.released && !use.token.AuthorizedAt.IsZero() {
			tokens = append(tokens, use.token)
		}
	}

	b.mu.Unlock()

	if len(tokens) > 0 {
		b.ClaimResolver.releaseDeliveryClaimsWithWarning(ctx, tokens, "Failed to release fully failed batch delivery claims")
	}
}
