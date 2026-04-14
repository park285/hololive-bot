# Hololive Kakao Bot 알람 런타임 개요

이 문서는 현재 운영 중인 YouTube/Chzzk/Twitch 알람 파이프라인의 실제 런타임 경로를 요약한다. 예전 bot ticker가 Holodex 일정 조회부터 발송까지 직접 처리하던 구조는 더 이상 현재 기준이 아니다.

## 1. 구성 요소

### 구독/설정 소유 seam
- `hololive/hololive-kakao-bot-go/internal/service/notification/alarm_service.go`
  - 방별 알람 CRUD
  - target minute 설정 보관
  - room/channel/member 캐시 갱신
  - checker가 발행 후 호출하는 `MarkUpcomingEventNotified` 제공

### 런타임 스케줄러 seam
- `hololive/hololive-kakao-bot-go/internal/service/alarm/scheduler/runtime_scheduler.go`
  - 플랫폼별 루프 실행
  - YouTube/Chzzk/Twitch checker 생성
  - shared dedup 서비스와 queue publisher를 notifier에 연결
  - runtime target minute 변경을 checker/dedup에 동기화

### checker seam
- YouTube: `hololive/hololive-kakao-bot-go/internal/service/alarm/checker/youtube_checker.go`
- Chzzk: `hololive/hololive-kakao-bot-go/internal/service/alarm/checker/chzzk_checker.go`
- Twitch: `hololive/hololive-kakao-bot-go/internal/service/alarm/checker/twitch_checker.go`
- 공용 helper:
  - `hololive/hololive-shared/pkg/service/alarm/checker/helpers.go`
  - `hololive/hololive-shared/pkg/service/alarm/tier`

### dedup / queue seam
- dedup: `hololive/hololive-shared/pkg/service/alarm/dedup/service.go`
- queue publish: `hololive/hololive-shared/pkg/service/alarm/queue/publisher.go`
- queue consume/requeue: `hololive/hololive-shared/pkg/service/alarm/queue/consumer.go`

### dispatcher seam
- `hololive/hololive-dispatcher-go/internal/dispatch/dispatcher.go`
  - 큐에서 envelope 배치 drain
  - room 기준 그룹핑
  - message render
  - Iris 전송
  - send 실패 시 requeue
  - render 실패 시 claim key release

## 2. 실제 알람 흐름

1. 사용자가 `AlarmService`를 통해 room 단위 구독을 저장한다.
2. `RuntimeScheduler`가 플랫폼별 주기로 checker를 실행한다.
3. checker가 외부 상태를 읽고 `[]*domain.AlarmNotification` 후보를 만든다.
   - YouTube는 Holodex live status, tier scheduler, evaluation window를 함께 사용한다.
   - target minute 판정은 shared checker helper가 담당한다.
4. `Notifier`가 후보를 순차 처리한다.
   - `dedup.TryClaimNotification`
   - `dedup.TryClaimLogicalEvent`
   - `queue.Publisher.Publish`
   - `dedup.MarkAsNotified`
   - `AlarmService.MarkUpcomingEventNotified`
5. `dispatcher-go`가 큐 envelope를 소비해 room별로 묶어 렌더링 후 Iris로 전송한다.

즉, 현재 구조의 핵심 경계는 `runtime scheduler -> checker -> notifier -> queue -> dispatcher` 이다.

## 3. YouTube 알람 의미론

YouTube upcoming 알람은 다음 입력으로 계산된다.

- 구독 채널 집합: `AlarmChannelRegistryKey`
- due channel 선정: `tier.TieredScheduler`
- 현재 라이브/예정 상태: Holodex live status
- target minute 정책: `sharedchecker.NormalizeTargetMinutes(...)`
- 평가 창: `sharedchecker.ResolveEvaluationWindow(...)`
- 중복 방지: `dedup.Service`

`YouTubeChecker.buildUpcomingNotifications(...)` 는 다음 순서를 따른다.

1. upcoming + `StartScheduled` 존재 여부 확인
2. evaluation window 안에서 crossed target 계산
3. `IsAlreadyNotifiedForSchedule(...)` 로 중복 차단
4. room별 `AlarmNotification` 생성

live catch-up 도 같은 checker 안에서 별도로 계산되지만, 발송 경로는 동일하게 notifier와 queue를 통과한다.

## 4. 중복 방지와 큐 계약

- checker 단계에서는 dedup claim이 먼저 일어난다.
- queue에는 `domain.AlarmQueueEnvelope` 가 적재된다.
- envelope 안에는 notification payload와 claim key가 함께 들어간다.
- dispatcher에서 render 실패 시 claim key를 해제한다.
- dispatcher에서 send 실패 시 envelope를 requeue 한다.

이 계약 덕분에 checker와 dispatcher는 다음 책임으로 분리된다.

- checker/notifier: “보내도 되는가”
- dispatcher: “어떻게 묶어서 실제로 보낼 것인가”

## 5. 현재 문서를 읽을 때의 주의점

- 예전 bot ticker 중심 설명은 현재 구조와 맞지 않는다.
- `AlarmService` 는 지금도 구독 저장과 일부 발송 상태 보조 마킹을 담당하지만, 플랫폼 알람 후보 계산의 주 루프는 `RuntimeScheduler` 와 각 checker가 소유한다.
- 최종 발송은 bot 프로세스가 직접 하지 않고 queue + `dispatcher-go` 를 거친다.

## 6. 빠른 코드 진입점

- 런타임 시작점: `internal/service/alarm/scheduler/runtime_scheduler.go`
- YouTube 후보 계산: `internal/service/alarm/checker/youtube_checker.go`
- 발행 경계: `internal/service/alarm/checker/notifier.go`
- 큐 publish/consume: `hololive-shared/pkg/service/alarm/queue/`
- 최종 발송: `hololive-dispatcher-go/internal/dispatch/dispatcher.go`
