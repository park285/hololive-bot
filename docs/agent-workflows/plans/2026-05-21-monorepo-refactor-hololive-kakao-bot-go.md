# 2026-05-21 — hololive-kakao-bot-go refactor (Phase 2.C.2)

Sub-plan of `2026-05-21-monorepo-refactor-master.md`. 본 sub-plan 은 모듈 내 LOC/함수 budget 정리, 네이밍 정리, 중복 추출 후 cross-cutting helper(2.B) 가 정의되어 있다면 그 helper 로의 마이그레이션까지 포함.

## Goal

`hololive/hololive-kakao-bot-go` 의 UNLISTED-LARGE 5건과 bootstrap 테스트 0% 영역을 정리하고, 약어/패키지-타입 이름 드리프트를 단일화한다.

## Inventory

`docs/agent-workflows/plans/_inventory-2026-05-21/01-hololive-kakao-bot-go.md`

## Target work

LOC / 함수 budget:
- `internal/service/streamfeed/service.go` (339, UNLISTED) — 멤버 캐시·머지 헬퍼 분리.
- `internal/bot/internal/orchestration/bot_transport.go` (327, UNLISTED) — Iris send/retry 책임 분리.
- `internal/adapter/internal/messaging/formatter_profile.go` (324, UNLISTED) — 섹션별 formatter 파일 분리.
- `internal/command/internal/handlers/handler_live.go` (306, UNLISTED) — Live/Upcoming 분리, Chzzk fallback 별도 함수.
- `internal/adapter/internal/messaging/formatter_streams.go` (302, UNLISTED) — 시간/이모지/레이아웃 helper 분리.
- `internal/command/internal/handlers/handler_alarm.go:129 handleAdd` (~50+ lines) — 멤버 해석/타입 파싱/그라쥬에이션 체크 분리.

테스트 보강:
- `internal/app/bootstrap/` 15 prod / 1 test — `InitBotInfrastructure`, 알람/YouTube 스택 와이어링, 멤버 캐시 부트스트랩 테스트.
- `internal/app/http/` 1 prod / 0 test — webhook 라우터 테스트.
- `internal/command/internal/handlers/` 미커버 핸들러(schedule, subscriber, stats, help, subscriber_graph, member_info group, news subscription).
- `internal/bot/internal/orchestration/` 미커버 8건.

네이밍 단일화:
- Cache 필드 이름 통일(`Cache` 채택, `cacheSvc`/`CacheSvc` 제거).
- Repository 표기 통일(약어 금지: `repo` → `repository`/`memberRepository`).
- `pgCfg` → `postgresConfig` 또는 `cfg`.
- `matcher.MemberMatcher` 의 패키지/타입 중복 해소.

중복 → cross-cutting helper 로:
- HTTP 서버 lifecycle: `internal/app/runtime/{http_server,lifecycle}.go` 를 Phase 2.B.3 helper 로.
- Signal/shutdown hook: 같은 helper 활용.
- 멤버 캐시 부트스트랩 통합 후보.

## File map

```
internal/service/streamfeed/                 # 분할 + 테스트 보강
internal/bot/internal/orchestration/         # transport/ingress/error 책임 분리, 테스트 추가
internal/adapter/internal/messaging/         # formatter_* 분할
internal/command/internal/handlers/          # handler_alarm 분해, handler_live/_subscriber/_stats/_help 테스트
internal/app/bootstrap/                      # 캐시·이름 통일, 테스트 추가
internal/app/runtime/                        # 2.B.3 lifecycle helper 적용
internal/app/wiring/                         # Repo/repository 명명 통일
```

## Validation

```bash
./build-all.sh --no-bump
go build ./hololive/hololive-kakao-bot-go/...
go test  ./hololive/hololive-kakao-bot-go/...
./scripts/architecture/ci-boundary-gate.sh
```

LOC gate (모듈 한정):
```bash
git diff --name-only main...HEAD | xargs -I{} awk 'END{print FILENAME, NR}' {} 2>/dev/null
```

## Stop rules

- 본 plan 변경으로 인해 변경 모듈 외부(예: `hololive-shared` 의 export 수정) 이 필요해지면 stop 후 master plan 으로 escalate.
- `internal/adapter` 의 외부 contract(Iris 응답 shape, 메시지 포맷) 변경 발견 시 stop.
- `handler_alarm` 분해 도중 멤버 매칭 동작 변경 가능성이 발견되면 분해 전 동작 보존 테스트 작성.

## Out of scope

- Iris webhook 의 외부 동작 변경.
- 멤버 데이터 모델 변경.
- Kakao 메시지 포맷의 사용자-시각적 변경.
