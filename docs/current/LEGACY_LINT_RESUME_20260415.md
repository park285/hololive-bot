# Legacy Lint Cleanup Resume Guide (2026-04-15)

상태: **CLOSED**

완료 메모 (2026-04-15):

- `golangci-lint run ./internal/service/acl/... ./internal/service/twitch/...` → `0 issues`
- `golangci-lint run ./pkg/providers/... ./pkg/service/youtube/...` → `0 issues`
- `go test ./internal/service/acl/... ./internal/service/twitch/... -count=1` → PASS
- `go test ./pkg/providers/... ./pkg/service/youtube/... -count=1` → PASS
- `go build ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-dispatcher-go/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/...` → PASS
- `go test ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-dispatcher-go/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/... -count=1` → PASS

이 문서는 2026-04-15 세션에서 남은 broader legacy lint debt 를
다음 세션에서 바로 이어서 정리할 수 있도록 남기는 재개 메모다.

핵심 원칙:

- `//nolint:...` 형태의 suppress 주석으로 해결하지 않는다.
- `.golangci.yml` 에 file-specific 예외를 추가해서 빼지 않는다.
- 가능한 한 **실제 리팩터링 / 테스트 분해 / helper 추출**로 해결한다.

---

## 1. 현재 dirty scope

현재 미커밋 변경은 아래 패키지 축에 모여 있다.

### `hololive-kakao-bot-go`

- `internal/service/acl/*`
- `internal/service/twitch/*`

### `hololive-shared`

- `pkg/providers/*`
- `pkg/service/youtube/*`

즉, 다음 세션은 위 4개 seam 에 집중하면 된다.

---

## 2. 현재 lint 스냅샷

아래 결과는 2026-04-15 마지막 기준이다.

### A. `hololive-kakao-bot-go/internal/service/acl` + `internal/service/twitch`

실행:

```bash
cd /home/kapu/gemini/hololive-bot/hololive/hololive-kakao-bot-go
golangci-lint run ./internal/service/acl/... ./internal/service/twitch/...
```

남은 축:

- `funlen`
  - `internal/service/twitch/client.go:getStreams`
  - `internal/service/acl/service_db_test.go:TestACLService_LoadFromDatabase_ReturnsInitCreateError`
- `gocognit`
  - `TestNewACLService_CacheSyncFailuresKeepLoadedStateAndLog`
  - `TestACLService_ReturnsCacheSyncErrorsOnMutations`
- `govet shadow`
  - `service_db_test.go` 내 여러 `if err := ...` 패턴
- `maintidx`
  - 일부 ACL 테스트 함수
- `revive`
  - unused parameter (`rooms`) 1건
- `thelper`
  - local closure helpers multiple
- `wrapcheck`
  - ACL test helper closures에서 내부 에러 반환
- `wsl_v5`
  - `acl/service.go`
  - `acl/service_db_test.go`
  - `acl/service_test.go`

### B. `hololive-shared/pkg/providers`

실행:

```bash
cd /home/kapu/gemini/hololive-bot/hololive/hololive-shared
golangci-lint run ./pkg/providers/...
```

현재 상태:

- **0 issues**

즉 providers 축은 현재 close 상태다.

### C. `hololive-shared/pkg/service/youtube`

실행:

```bash
cd /home/kapu/gemini/hololive-bot/hololive/hololive-shared
golangci-lint run ./pkg/service/youtube/...
```

남은 축:

- `dupl`
  - `scheduler_alerts.go`
- `funlen`
  - `outbox/delivery_telemetry_repository.go`
  - `outbox/dispatcher_send.go`
  - `poller/published_at_resolver_repository.go`
  - `tracking/alarm_state_repository.go`
  - `tracking/repository_identity.go`
- `gocognit`
  - `poller/community_poller.go`
  - `poller/published_at_resolver.go`
  - `poller/published_at_resolver_repository.go`
  - `poller/repository_batch.go`
  - `poller/repository_batch_delivery_state.go`
  - `poller/shorts_poller.go`
- `gocritic`
  - importShadow / rangeValCopy
- `gocyclo`
  - `outbox/dispatcher_claim_gate.go`
  - `outbox/dispatcher_send.go`
  - `poller/helpers.go`
  - `poller/repository_batch_alarm_state.go`
  - `tracking/observation_window_repository.go`
- `gosec`
  - `dispatcher_send.go` 의 indexed slice access (`G602`)
- `govet`
  - shadow 3건
- `modernize`
  - `delivery_latency_period_rollups.go`
- `prealloc`
  - `dispatcher_claim_gate.go`
- `unused`
  - `contentid/canonical.go`
  - `poller/published_at_resolver.go`
  - `poller/repository_batch_alarm_state.go`
- `wastedassign`
  - `outbox/dispatcher.go`
- `wrapcheck`
  - transaction / ctx.Err / downstream method return wrapping 10건

---

## 3. 이미 합의된 작업 원칙

이 세션에서 사용자와 합의된 방향:

1. suppress 주석으로 덮지 않는다.
2. 룰 파일에서 예외로 빼지 않는다.
3. 가능하면 아래 순서로 푼다.

### Pass order

#### Pass 1 — ACL / Twitch

목표:

- 큰 table test 2~3개를 실제 개별 테스트 + helper로 분해
- closure helper를 top-level helper로 끌어올려 `thelper` 제거
- `shadow` / `wrapcheck` / `wsl_v5` 를 실제 코드 정리로 제거
- `getStreams` 를 request/status/decode helper로 분해

#### Pass 2 — `scheduler_alerts.go`

목표:

- milestone / approaching dispatch 공통 seam 추출
- `dupl` 제거

#### Pass 3 — `pkg/service/youtube` correctness-adjacent lint

우선순위:

1. `gosec` (`dispatcher_send.go`)
2. `govet shadow`
3. `wrapcheck`
4. `unused`
5. `wastedassign`

#### Pass 4 — `pkg/service/youtube` complexity reduction

우선순위:

1. `funlen`
2. `gocognit`
3. `gocyclo`
4. `modernize`
5. `prealloc`

---

## 4. Behavior lock / proof already green

다음 커맨드는 이미 green 상태였고, 다음 세션에서도 수정 전후 회귀 기준으로 재사용하면 된다.

```bash
cd /home/kapu/gemini/hololive-bot/hololive/hololive-kakao-bot-go
go test ./internal/service/acl/... ./internal/service/twitch/... -count=1

cd /home/kapu/gemini/hololive-bot/hololive/hololive-shared
go test ./pkg/providers/... ./pkg/service/youtube/... -count=1
```

즉 다음 세션은:

- 먼저 위 테스트를 다시 실행해 baseline 을 확인하고
- 그 다음 각 pass 마다 targeted lint + targeted test 를 반복하면 된다.

---

## 5. 추천 재개 커맨드

### Step 1. ACL/Twitch 현재 남은 lint 재확인

```bash
cd /home/kapu/gemini/hololive-bot/hololive/hololive-kakao-bot-go
golangci-lint run ./internal/service/acl/... ./internal/service/twitch/...
```

### Step 2. ACL/Twitch 수정 후 검증

```bash
go test ./internal/service/acl/... ./internal/service/twitch/... -count=1
golangci-lint run ./internal/service/acl/... ./internal/service/twitch/...
```

### Step 3. YouTube 현재 남은 lint 재확인

```bash
cd /home/kapu/gemini/hololive-bot/hololive/hololive-shared
golangci-lint run ./pkg/service/youtube/...
```

### Step 4. YouTube 수정 후 검증

```bash
go test ./pkg/service/youtube/... -count=1
golangci-lint run ./pkg/service/youtube/...
```

### Step 5. 최종 broader verification

```bash
cd /home/kapu/gemini/hololive-bot
go build ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-dispatcher-go/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/...
go test ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-dispatcher-go/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/... -count=1
```

---

## 6. Stop condition

이 문서의 종료 기준은 아래다.

- `golangci-lint run ./internal/service/acl/... ./internal/service/twitch/...` → `0 issues`
- `golangci-lint run ./pkg/providers/...` → `0 issues` 유지
- `golangci-lint run ./pkg/service/youtube/...` → `0 issues`
- 관련 package test/build green
- suppress 주석 / config exclusion 추가 없음

이 기준은 2026-04-15 세션에서 모두 충족되었고, 본 문서는 종료 기록으로 보존한다.
