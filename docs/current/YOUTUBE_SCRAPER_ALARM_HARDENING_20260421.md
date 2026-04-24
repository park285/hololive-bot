# YouTube scraper / alarm delivery hardening plan — 2026-04-21

## 적용 배경

기존 번들에는 이미 “전체 멤버 111개를 매 주기마다 모두 긁어서 지연되는 문제”를 줄이기 위한 개선이 들어가 있었다. 이번 변경은 그 다음 단계로, YouTube HTML 수집과 알림 발송이 실패했을 때 더 안전하게 감속하고, 실패 작업이 너무 오래 워커를 붙잡거나 다음 알림을 막지 않도록 하는 데 초점을 맞췄다.

핵심 목표는 다음과 같다.

1. YouTube가 `Retry-After`를 내려줄 때 그 신호를 실제 쿨다운에 반영한다.
2. 408/425/5xx/전송 계층 장애를 일시 장애로 분류하고, 429/403 장기 차단과 분리한다.
3. 폴러 1회 실행이 오래 멈춰도 워커가 무한 점유되지 않게 한다.
4. 실패한 폴러는 전체 정기 주기까지 기다리지 않고 짧은 백오프로 빠르게 재확인한다.
5. 알림 발송은 개별 방 단위 타임아웃을 갖도록 하여 Iris/Kakao 쪽 지연이 전체 배송 루프를 잡아먹지 않게 한다.
6. 구독자 조회 병렬도를 설정값으로 관리해 운영 환경에 맞게 조절할 수 있게 한다.

## 실제 코드 변경

### 1. YouTube scraper HTTP 실패 처리

수정 파일:

- `hololive/hololive-shared/pkg/service/youtube/scraper/client.go`
- `hololive/hololive-shared/pkg/service/youtube/scraper/backoff_state.go`

변경 내용:

- `Retry-After` 헤더를 초 단위 또는 HTTP 날짜 형식으로 파싱한다.
- 429/403 응답에서 `Retry-After`가 있으면 hard cooldown에 반영한다.
- 408 Request Timeout, 425 Too Early를 일시 장애 재시도 대상으로 추가했다.
- 기존 500/502/503/504는 그대로 재시도 대상으로 유지했다.
- HTTP 오류 응답에서도 body를 일부 drain한 뒤 close하여 커넥션 재사용 안정성을 높였다.
- hard backoff와 transient backoff를 분리한 기존 설계를 유지하면서, 외부가 제안한 쿨다운을 clamp해서 적용한다.

쿨다운 clamp 기준:

- hard cooldown 제안값: 최소 30초, 최대 6시간
- transient cooldown 제안값: 최소 5초, 최대 10분

이렇게 한 이유는 YouTube가 짧은 `Retry-After`를 줄 때 무조건 30분을 쉬는 과잉 감속을 줄이면서도, 악성/잘못된 헤더 값으로 인해 너무 자주 재시도하는 상황을 막기 위해서다.

### 2. Poller scheduler 안정화

수정 파일:

- `hololive/hololive-shared/pkg/service/youtube/poller/scheduler.go`
- `hololive/hololive-shared/pkg/providers/scraper_scheduler_options.go`
- `hololive/hololive-shared/pkg/providers/youtube_providers.go`
- `hololive/hololive-stream-ingester/internal/runtime/stream_ingester_youtube_components.go`
- `hololive/hololive-shared/pkg/config/config.go`
- `hololive/hololive-shared/pkg/config/config_types.go`

변경 내용:

- 폴러 1회 실행에 기본 45초 타임아웃을 추가했다.
- 폴러 실패 시 다음 정기 실행까지 기다리지 않고 짧은 재시도 백오프를 적용한다.
- 실패가 누적되면 30초 → 60초 → 120초처럼 증가하고, 기본 최대 5분에서 멈춘다.
- 실패 이후 성공하면 실패 카운터를 초기화하고 원래 채널별 분산 offset 기준으로 다시 정렬한다.
- 스케줄러 런타임 설정을 환경변수로 조절할 수 있게 했다.

추가 환경변수:

```env
SCRAPER_SCHEDULER_POLL_TIMEOUT_SECONDS=45
SCRAPER_SCHEDULER_ERROR_BACKOFF_MIN_SECONDS=30
SCRAPER_SCHEDULER_ERROR_BACKOFF_MAX_SECONDS=300
```

권장 운영값:

- 일반 운영: `45 / 30 / 300`
- YouTube 응답이 자주 느린 날: `60 / 60 / 600`
- 장애 원인 분석 중 빠른 재확인이 필요한 임시 운영: `30 / 10 / 120`

### 3. Outbox alarm delivery 안정화

수정 파일:

- `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher.go`
- `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send.go`

변경 내용:

- 방 단위 메시지 발송에 기본 10초 타임아웃을 추가했다.
- Iris/Kakao 발송이 멈추거나 매우 느릴 때 배송 워커가 무기한 잡히지 않는다.
- 구독자 조회 병렬도를 `SubscriberLookupParallelism` 설정값으로 분리했다.
- 기존 hardcoded `16`은 기본값으로 유지하되, 테스트/운영에서 조절 가능하게 했다.

기본값:

```go
DeliveryParallelism:         4,
DeliverySendTimeout:         10 * time.Second,
SubscriberLookupParallelism: 16,
```

### 4. 테스트 추가

추가/수정 파일:

- `hololive/hololive-shared/pkg/service/youtube/scraper/retry_test.go`
- `hololive/hololive-shared/pkg/service/youtube/poller/scheduler_test.go`
- `hololive/hololive-shared/pkg/service/youtube/outbox/dispatcher_send_test.go`
- `hololive/hololive-shared/pkg/config/config_test.go`

검증 항목:

- `Retry-After` 초 단위/HTTP 날짜 파싱
- 408/425 재시도 대상 분류
- 429 응답의 `Retry-After` 기반 cooldown 반영
- 폴러 실패 후 짧은 백오프 재스케줄
- 실패 복구 후 원래 offset 기반 재정렬
- 메시지 발송 타임아웃
- 스케줄러 환경변수 기본값/override/검증 오류

## 운영 확인 포인트

배포 후 로그에서 아래 메시지를 확인한다.

```text
Hard backoff activated
Transient backoff activated
Poll timed out
Poll job rescheduled after failure
YouTube rate limit hit, entering cooldown
send delivery message timed out
```

정상 흐름에서는 다음 특성이 보여야 한다.

- 429가 발생해도 같은 URL/채널을 즉시 반복 타격하지 않는다.
- 5xx나 네트워크 오류는 장기 차단으로 오인하지 않는다.
- 특정 채널 폴러가 실패해도 전체 poller queue가 장시간 멈추지 않는다.
- 특정 방 메시지 발송이 늦어도 다른 방 배송이 계속 진행된다.
- 실패 후 복구되면 스케줄이 다시 채널별 분산 offset으로 돌아간다.

## 추가로 이어서 개선할 방향

이번 변경은 코드 구조를 크게 흔들지 않는 안정화 패치다. 더 정교하게 가려면 아래 순서가 좋다.

### 1. 채널별 circuit breaker

현재 `BackoffState`는 scraper client 단위 전역 쿨다운이다. YouTube 전체 rate limit에는 맞지만, 특정 채널 페이지만 반복 실패하는 경우에는 전역 쿨다운이 과할 수 있다.

다음 단계에서는 `channelID + pollerName` 단위로 작은 circuit breaker를 두고, 전역 429/403과 채널별 404/5xx를 분리하는 게 좋다.

### 2. 알림 발송 idempotency key 전달

현재 outbox에는 `youtube-notification:<kind>:<contentID>` dedupe key가 있지만, `delivery.MessageSender` 인터페이스가 dedupe key를 발송 계층까지 넘기지 않는다. Iris 쪽에서 idempotency key를 받을 수 있다면 `SendMessage(ctx, roomID, message, dedupeKey)` 형태로 확장해 최종 발송 중복까지 더 단단히 막을 수 있다.

### 3. PublishedAt resolver 완전 분리

기존 리뷰 문서에도 남아 있던 과제다. `published_at` 보강은 알림 속도보다 덜 급하므로, shorts/community 신규 감지 루프와 완전히 분리된 저속 큐에서 처리하면 신규 감지 지연을 더 줄일 수 있다.

### 4. 관측 지표 보강

현재 로그 중심으로도 추적 가능하지만, 운영 안정성을 높이려면 다음 지표가 있으면 좋다.

- poller별 timeout 횟수
- poller별 consecutive failure gauge
- hard/transient cooldown remaining gauge
- delivery send timeout count
- subscriber lookup failure count
- outbox claim → delivery sent까지의 p50/p95 지연

### 5. YouTube HTML parser 회귀 샘플 고정

YouTube HTML 구조는 계속 바뀐다. shorts/community parser에 실제 HTML fixture를 추가해 “감지 0건”이 parser 문제인지 실제 0건인지 빠르게 분리할 수 있게 만드는 게 좋다.
