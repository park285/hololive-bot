# Alarm / Dispatch Remediation Guide (2026-04-14)

상태: **CLOSED / HISTORICAL (ownership moved on 2026-04-16)**

이 문서의 remediation 내용은 유효하지만, 여기서 언급하는 `hololive-kakao-bot-go/internal/service/notification/*` 구현은
이후 `hololive-shared/pkg/service/notification/*` 으로 이동했다.
현재 코드를 읽을 때는 새 shared ownership 경로를 기준으로 봐야 한다.


이 문서는 2026-04-14 기준 알람/디스패처 경로의 통합 보강 내용을 설명하는 현재 문서다.

## 범위

이번 보강안은 다음 네 축을 닫는다.

1. `render failure` 가 재시도 없이 유실되는 문제
2. subscriber cache rebuild 시 registry 밖 orphan room key 가 남는 문제
3. alarm cleanup 경로에서 `DoMulti` / `SCARD` / `DEL` 실패가 조용히 묻히는 문제
4. startup warm-up 시 set write 가 개별 `SADD` 위주로 수행되어 round-trip 이 과도한 문제

추가로 queue poison payload 보존과 얇은 clone helper 중복 제거도 함께 반영한다.

## 이미 현재 번들에서 해결된 것

다음 항목은 이번 번들 기준으로 이미 해결된 상태로 본다.

- 초기 capped observation 에서 5분 전 알람이 누락되던 의미론 버그
- persisted target minute healing 관련 기본 안전장치
- persistence executor 포화 시 inline fallback 처리

따라서 이번 문서는 5분 전 알람 버그 재수정이 아니라, 그 이후에도 남아 있던 운영 리스크를 정리하는 문서다.

## 파일별 변경 이유

### 1) `hololive-dispatcher-go/internal/dispatch/dispatcher.go`

기존에는 `renderer.RenderGroup()` 실패 시 claim key 만 해제하고 envelope 를 버렸다.
즉, 템플릿 회귀나 데이터 shape mismatch 가 발생하면 알림이 재시도 없이 사라졌다.

수정 원칙은 다음과 같다.

- `render` 실패도 `send` 실패와 동일한 durable retry / DLQ 정책을 탄다.
- retry metadata 의 `LastError` 에 실패 종류를 남긴다.
- 로그와 metrics 에 `failure_kind` 를 남겨 render/send 를 구분한다.

### 2) `hololive-shared/pkg/service/alarm/queue/consumer.go`

기존에는 다음 두 종류의 poison payload 가 조용히 사라질 수 있었다.

- active queue 에 들어온 invalid JSON payload
- delayed retry queue 에 들어온 손상된 wrapper member

수정 원칙은 다음과 같다.

- 파싱 불가 payload 는 버리지 않고 raw payload 그대로 DLQ 로 보존한다.
- delayed retry wrapper 손상도 raw member 그대로 DLQ 로 보존한다.
- 정상 payload draining 은 계속 진행한다.

### 3) `hololive-shared/pkg/service/alarm/cache_warm.go`

기존 rebuild 는 `alarm:registry` 만 신뢰하고 `alarm:{roomID}` 키를 삭제했다.
부분 실패나 수동 조작이 있으면 registry 밖 orphan room key 가 남아 stale subscriber cache 가 유지될 수 있었다.

수정 원칙은 다음과 같다.

- `alarm:*` namespace 를 스캔하되 dispatch / registry / typed subscriber / hash key 는 제외한다.
- 실제 room alarm set 으로 보이는 key 는 rebuild 전에 같이 제거한다.
- warm-up set write 는 `DoMulti()` batched `SADD` 로 보낸다.

### 4) `hololive-kakao-bot-go/internal/service/notification/alarm_service.go`

기존 cleanup helper 는 result count mismatch, parse error, delete error 를 사실상 성공처럼 처리했다.
그 결과 channel registry 를 너무 일찍 지우거나 stale subscriber set 을 남길 수 있었다.

수정 원칙은 다음과 같다.

- cleanup helper 들을 `error` 반환형으로 바꾼다.
- 상위 호출부에서 primary path 는 유지하되 경고 로그를 남긴다.
- cleanup 실패를 성공처럼 조용히 넘기지 않는다.

### 5) `hololive-kakao-bot-go/internal/app` / `internal/bot`

`command_builder_clone.go` 가 app 과 bot 양쪽에 중복으로 있었다.
이건 작은 helper 라도 중복 정의가 누적되는 전형적 냄새이므로 `bot.CloneCommandBuilders()` 하나로 정리한다.

## 운영 기대 효과

- render 회귀가 나도 알림이 조용히 유실되지 않는다.
- rebuild 후 stale room cache key 가 남지 않는다.
- alarm remove / clear 이후 registry 정합성이 무너지는 silent corruption 가능성이 줄어든다.
- startup warm-up 시 cache write round-trip 이 줄어든다.
- queue poison payload 를 사후 포렌식 가능한 형태로 보존한다.

## 남는 후속 과제

이번 보강안으로도 retry queue 전체 의미론이 완전히 transactional 해지는 것은 아니다.
특히 delayed retry member 를 dequeue 한 뒤 프로세스가 비정상 종료되면 중복/유실 없는 완전한 ack 모델은 아직 아니다.
이 부분은 별도 설계 변경으로 다루는 것이 맞다.

권장 후속 주제:

- retry queue reserve/ack 설계
- dispatcher envelope lifecycle metrics 세분화
- `internal/app` 부트스트랩 경계 분리
- `*_additional_test.go` 정리 및 책임 기준 재배치
