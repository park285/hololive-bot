// Package outbox provides the public facade for YouTube notification outbox
// delivery, telemetry, formatting, and dispatcher construction.
//
// 외부 surface 정책
//
// outbox 패키지의 외부 API 는 모두 internal/delivery 의 type, const, func,
// error 를 alias 또는 re-export 한 surface 다. 외부 모듈은 admin-api,
// alarm-worker, youtube-producer 처럼 outbox 패키지를 import 하고 이 surface 를
// 사용한다. internal/delivery 패키지는 구현 본거지이며, Go internal package
// 규칙상 외부 모듈의 직접 import 대상이 아니다.
//
// 주요 사용 패턴
//
//	import outbox "github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
//
//	func builder(ctx context.Context, repo outbox.DeliveryTelemetryRepository) ([]outbox.PostSendCount, error) {
//		return repo.ListPostSendCountsSince(ctx, time.Now().Add(-24*time.Hour))
//	}
//
// 내부 helper 정책
//
// internal/delivery 는 dispatcher, repository, formatter 의 실제 구현을 가진다.
// 외부에 노출할 계약은 outbox 패키지의 alias 와 re-export 로만 올린다. 외부 surface
// 가 아닌 internal helper 는 outbox 패키지에 alias 를 추가하지 않는다.
package outbox
