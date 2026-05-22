# 2026-05-21 — shared-go refactor (Phase 2.A.1)

Sub-plan of `2026-05-21-monorepo-refactor-master.md`. 본 모듈은 모노리포 가장 하위 공용 라이브러리이므로 **2.A 단계의 첫 작업**으로 진행한다.

## Goal

shared-go 의 임계 파일 stale baseline 정정, 미커버 유틸 테스트 보강, context/logging/operation 헬퍼의 표면 정리. Phase 2.B cross-cutting helper(`runTickerLoop`, `nextExponentialBackoff`, `logAndWrapError`) 의 본거지 후보로서의 표면 확정.

## Inventory

`docs/agent-workflows/plans/_inventory-2026-05-21/07-shared-go.md`

## Target work

LOC / baseline:
- `docs/architecture/file-loc-thresholds.txt` 에서 `shared-go/pkg/logging/logging.go:520` → 실제 215. baseline 재설정.
- `pkg/logging/logging_archive.go` 320 — archive helper 의 receiver method 화 또는 책임 분리.
- `pkg/telemetry/telemetry.go:56 NewProvider` ~56라인 — setup/sampler 분리.

테스트 보강:
- `pkg/logging/operation.go` (94 LOC) — `RunOperation`, `operationContext*`, `operationName`, `operationAttrs`, `eventOrDefault` 7개 함수 테스트.
- `pkg/logging/log.go` (56 LOC) — context attrs Debug/Info/Warn/Error wrapper 단위 테스트.
- `pkg/logging/id.go` (56 LOC) — ID 생성 + prefix sanitization 테스트.
- `pkg/jsonutil/extract.go` — fence/bracket fallback edge case 테스트(잘못된 JSON 입력 다양화).
- `pkg/httputil/client.go` — `applyTransportProfile`, `baseProfiledTransport`, 타임아웃 profile composition 테스트.

네이밍 단일화:
- `sanitizeIDPrefix`, `errorType`, `withString` 명명을 helper 패턴 컨벤션에 정렬(예: `withString` → `withStringAttr` 또는 receiver 메서드).
- context helper `With*` / `*FromContext` 컨벤션 통일.
- `compressedLogArchiver` 의 standalone helpers 를 receiver method 로 옮길지 결정.
- `jsonBracketMatcher` (unexported) + `newJSONBracketMatcher` (case 일치 검토).
- HTTP errors `newAPIError` vs `CheckStatus` surface — 외부 노출 일관성.

중복 → 정리:
- `httputil/{client,json_client}.go` 의 transport profile/timeout 로직과 `hololive-shared/pkg/server/internal/httpserver/runtime_helpers.go` 의 timeout/profile 코드 — 어느 쪽이 정본인지 결정.
- `logging/context.go` (With*FromContext) + `logging/operation.go` (operationContext*) 의 컨텍스트 carrier 정리.
- `telemetry/MapCarrier` + `logging/OTelHandler` 의 span 전파 — 단일 propagation 레이어.
- `httputil/response.go APIError` + `logging/attrs.go ErrorAttrs` 의 error→attr 변환 — 단일 helper.
- ID 생성 + operation 초기화 — `NewOperationID(prefix)` factory.

Cross-cutting helper 표면 결정(2.B 진입 전 필수):
- `runTickerLoop(ctx, interval, onTick func(context.Context) error)` 시그니처 후보.
- `nextExponentialBackoff(current, max, step time.Duration) time.Duration` 시그니처 후보.
- `logAndWrapError(ctx, logger *slog.Logger, op string, err error, attrs ...slog.Attr) error` 시그니처 후보 — slog 컨벤션 침해 회피를 위해 attrs 인자 형태 검토.

## File map

```
pkg/logging/                                         # operation/log/id 테스트, archive 책임 정리
pkg/telemetry/                                       # NewProvider 분해
pkg/jsonutil/                                        # extract 엣지 케이스 테스트
pkg/httputil/                                        # client/profile 테스트
docs/architecture/file-loc-thresholds.txt             # logging.go baseline 정정
pkg/runtime/                                         # 신규: runTickerLoop / runIsolatedLoop / backoff helper 시그니처 결정
```

## Validation

```bash
./build-all.sh --no-bump
go build ./shared-go/...
go test  ./shared-go/...
go test -cover ./shared-go/pkg/logging/...
./scripts/architecture/ci-boundary-gate.sh
```

## Stop rules

- 로그 키 schema 변경이 alert/grep 룰 영향이 의심되면 stop.
- httputil profile 통합이 hololive-shared 측 동작을 바꾸면 stop, hololive-shared sub-plan 으로.
- helper 시그니처 결정이 길어지면 별도 mini-plan 으로 brainstorming → 합의 후 진입.

## Out of scope

- otel exporter endpoint/포맷 변경.
- 로그 출력 포맷(외부 alert/소비자 영향) 변경 자체가 목적인 작업.
