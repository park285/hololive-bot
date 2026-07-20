// Package claim은 reuse cache abstraction(ClaimKey, ClaimStatus, ReuseCache)을 제공한다.
//
// 외부 surface
//
// claim 패키지는 claim 토큰과 cool-down hint 를 함께 다루는 작은 cache
// abstraction 이다. 호출부는 ClaimKey 로 도메인과 대상 식별자를 고정하고,
// holder 와 ttl 로 단일 처리자 claim 을 요청한다.
//
// 주요 사용 패턴
//
//	import "github.com/kapu/hololive-shared/internal/service/cache/claim"
//
//	func acquire(ctx context.Context, cache claim.ReuseCache, videoID string) (claim.ClaimStatus, error) {
//		key := claim.ClaimKey{Scope: "youtube_outbox_delivery", Subject: videoID}
//		return cache.Claim(ctx, key, "worker-a", time.Minute)
//	}
package claim
