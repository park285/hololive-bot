# 2026-05-21 — hololive-alarm-worker refactor (Phase 2.C.4)

Sub-plan of `2026-05-21-monorepo-refactor-master.md`.

## Goal

`youtube_checker.go` 93%, `notifier.go` 89% 의 LOC 압박을 해소하고, `checker` ↔ `checking` 패키지 분할 모호성을 정리한다. 디스패치 러너 테스트 부재 영역을 메운다.

## Inventory

`docs/agent-workflows/plans/_inventory-2026-05-21/03-hololive-alarm-worker.md`

## Target work

LOC / 함수 budget:
- `internal/service/alarm/checker/internal/checking/youtube_checker.go` 447/480 NEAR — input/persisted-live/stream-helpers split.
- `internal/service/alarm/checker/internal/checking/notifier.go` 408/460 NEAR — publish/prepare 분리.
- `internal/service/alarm/checker/internal/checking/chzzk_checker.go` 395/UNLISTED — lookup-job 분리.
- `internal/app/internal/workerapp/alarm_dispatch_karing.go` 393/UNLISTED — community/template 분리.
- `internal/service/alarm/checker/internal/checking/common.go` 349/UNLISTED — cache bootstrap 분리.
- 함수: `dispatchGroup`(~50+), `buildAlarmFoundation`(~56), `NewRuntimeScheduler`(~55) 분해.

테스트 보강:
- `internal/app/internal/workerapp` 17 prod / 6 test — dispatch_runner_loop/idle/karing_community, build_egress 테스트.
- `internal/service/alarm/scheduler` 5 prod / 2 test — events/cache_recovery/twitch 테스트.
- `internal/service/alarm/checker/internal/checking` 15 prod / 8 test — youtube_checker_*, chzzk_checker_lookup_job, notifier_publish 테스트.

네이밍 단일화:
- `checker/checker.go` 의 type alias 재export 정책: 유지 시 alias 만 둔 파일임을 doc.go 에 명시, 축소 시 외부 노출 surface 1개로 정리.
- `alarmDispatchSender` vs `alarmDispatchClientRequestSender` — composability 기준으로 단일 인터페이스 + variant 메서드로 통일 또는 명시적 별개 책임으로 doc.
- 플랫폼 명사 케이스 정렬: `ChzzkChecker`/`TwitchChecker`/`YouTubeChecker` 한 컨벤션.
- `alarmDispatchKaring*` 상수 prefix 중복 — 파일 scope const 블록으로.

중복 → cross-cutting:
- Signal/shutdown lifecycle → Phase 2.B.3.
- Error wrap + log → Phase 2.B.4 (단, 변경 분량 폭주 주의 — 단일 PR scope 제한).
- Dedup claim/release → Phase 2.B.2.
- Queue envelope consumer loop (`alarm_dispatch_runner.go:57–65`) ↔ `hololive-shared/pkg/service/alarm/queue/consumer` — `hololive-shared` 측 abstraction 으로 흡수 후 alarm-worker 가 사용.
- Valkey/cache 부트스트랩 — `common.go:35` 의 valkey 직접 import 을 hololive-shared cache 팩토리로.
- HTTP server wrapper → Phase 2.B.3.

## File map

```
internal/service/alarm/checker/                   # 패키지 분할 결정 + 파일 split
internal/service/alarm/checker/internal/checking/ # youtube/chzzk/notifier 분해
internal/service/alarm/scheduler/                  # events/cache_recovery/twitch 테스트
internal/app/internal/workerapp/                   # dispatch_runner_* 테스트, error wrap helper 도입
internal/app/runtime/                              # lifecycle helper 적용
```

## Validation

```bash
./build-all.sh --no-bump
go build ./hololive/hololive-alarm-worker/... ./hololive/hololive-shared/...
go test  ./hololive/hololive-alarm-worker/... ./hololive/hololive-shared/...
./scripts/architecture/ci-boundary-gate.sh
```

## Stop rules

- 디스패치 동작/순서/dedup 의미 변화가 의심되면 stop 후 회귀 테스트 우선.
- `checker`/`checking` 패키지 재구성으로 외부 import 가 깨지면 별도 호환 PR 로 분리.
- `hololive-shared` 의 queue consumer abstraction 변경이 필요하면 Phase 2.A 또는 별도 sub-plan.

## Out of scope

- 알림 전송 채널(이메일/IRC/Kakao) 추가/제거.
- Retry/backoff 정책의 의미 변경.
- 외부 서비스(YouTube/Chzzk/Twitch) API 호출 형식 변경.
