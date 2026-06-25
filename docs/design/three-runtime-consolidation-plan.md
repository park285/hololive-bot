# Three Runtime Consolidation Plan

## 상태

이 문서는 아직 `docs/current` SSOT가 아닙니다. 현재 운영 구조는 5개 Go runtime이며, 본 문서는 3개 runtime으로 줄이기 위한 코드 레벨 목표와 migration guardrail을 고정합니다.

목표 runtime은 다음 3개입니다.

| Target runtime | Absorbs | Must remain separate |
|---|---|---|
| `hololive-api` | `bot`, `admin-api`, `llm-scheduler` | - |
| `alarm-worker` | alarm HTTP provider, alarm scheduler/checker, proactive egress, delivery/outbox drain | `youtube-producer` scraping/outbox production |
| `youtube-producer` | YouTube polling/scraping, active-active AP coordination, `youtube_notification_outbox` production, photo sync singleton | Iris/Kakao egress |

핵심 결정은 단순히 Compose service 수만 줄이는 것이 아니라, `bot`, `admin-api`, `llm-scheduler`를 하나의 Go process 안에 세 logical plane으로 통합하는 것입니다. 하나의 컨테이너에서 3개 바이너리를 supervisor로 실행하는 방식은 금지합니다. 그 방식은 장애 전파, signal handling, log rotation, readiness, memory accounting을 불명확하게 만들어 MSA 경계를 줄인 효과보다 운영 리스크가 큽니다.

## 현재 제약

현재 각 runtime은 독립 Go module이며 핵심 runtime package가 각 module의 `internal` 아래에 있습니다.

- `hololive/hololive-kakao-bot-go/internal/app/botruntime`
- `hololive/hololive-admin-api/internal/app`
- `hololive/hololive-llm-sched/internal/app/internal/runtime`

따라서 새 module에서 기존 runtime을 단순 import하는 방식은 Go `internal` import rule 때문에 불가능합니다. 우회 import나 symlink로 해결하지 않습니다. 3-runtime 전환은 반드시 code ownership 이동을 동반해야 합니다.

## 최종 코드 구조

최종 구조는 다음을 목표로 합니다.

```text
hololive/hololive-api/
  go.mod
  cmd/hololive-api/main.go
  internal/app/runtime.go
  internal/app/runtime_start.go
  internal/app/runtime_shutdown.go
  internal/app/config.go
  internal/planes/bot/
  internal/planes/admin/
  internal/planes/llm/
  internal/platform/http/
  internal/platform/infra/
  internal/platform/observability/

hololive/hololive-alarm-worker/
  cmd/alarm-worker/main.go
  internal/app/workerapp/
  internal/http/alarm/

hololive/hololive-youtube-producer/
  cmd/youtube-producer/main.go
  internal/...
```

`hololive-api` 내부 plane은 같은 process에 있지만 서로의 implementation package를 직접 import하지 않습니다. 공통 기능은 `hololive-shared` 또는 `internal/platform`으로만 통과합니다.

허용 import 방향은 다음과 같습니다.

```text
cmd/hololive-api
  -> internal/app
internal/app
  -> internal/planes/bot
  -> internal/planes/admin
  -> internal/planes/llm
  -> internal/platform/*
internal/planes/*
  -> internal/platform/*
  -> hololive-shared/pkg/*
```

금지 import 방향은 다음과 같습니다.

```text
internal/planes/bot   -> internal/planes/admin
internal/planes/bot   -> internal/planes/llm
internal/planes/admin -> internal/planes/bot
internal/planes/admin -> internal/planes/llm
internal/planes/llm   -> internal/planes/bot
internal/planes/llm   -> internal/planes/admin
hololive-api          -> hololive-alarm-worker/internal/*
hololive-api          -> hololive-youtube-producer/internal/*
```

## `hololive-api` process model

`hololive-api`는 하나의 process 안에서 3개 listener/plane을 띄웁니다.

```text
hololive-api process
  ├─ bot plane   :30001  Kakao/Iris webhook, user command routing
  ├─ admin plane :30006  dashboard/admin HTTP control plane
  └─ llm plane   :30003  membernews/majorevent/trigger internal API and schedulers
```

초기 migration에서는 기존 route와 port를 유지합니다. 외부 dependency, dashboard 설정, internal URL, healthcheck가 동시에 흔들리지 않게 하기 위해서입니다.

Compose network alias도 rollout 기간에는 유지합니다.

```yaml
networks:
  hololive-net:
    aliases:
      - hololive-bot
      - hololive-admin-api
      - llm-scheduler
```

이 alias는 이전 internal URL을 깨지 않기 위한 compatibility layer입니다. 최종적으로는 `LLM_SCHEDULER_INTERNAL_URL` 같은 self-call을 제거하고 in-process client interface로 전환합니다. 단, 첫 rollout에서는 기존 HTTP contract를 유지하여 변경면을 줄입니다.

## config 결정

최종 config loader는 `LoadHololiveAPIRuntime`을 새로 둡니다. 기존 `LoadBotRuntime`, `LoadAdminAPIRuntime`, `LoadLLMSchedulerRuntime`를 단순히 세 번 호출하지 않습니다. 그 방식은 PostgreSQL, Valkey, logging, cert reload, metrics 설정을 중복 초기화할 가능성이 높습니다.

새 config shape는 다음 원칙을 따릅니다.

```go
type HololiveAPIConfig struct {
    Common CommonRuntimeConfig
    Bot    BotPlaneConfig
    Admin  AdminPlaneConfig
    LLM    LLMPlaneConfig
}
```

`Common`에는 process-level singleton을 둡니다.

- PostgreSQL connection config
- Valkey connection config
- logging config
- H3 certificate paths and reload policy
- API secret key
- process-level memory/GC knobs

각 plane config에는 plane-specific setting만 둡니다.

- bot: Kakao/Iris webhook, command prefix, ACL, bot metrics/pprof ports
- admin: CORS, allowed IPs, dashboard auth/session, admin metrics port
- llm: LLM/cliproxy/exa, schedule interval, feed scraper schedule, llm metrics port

기존 env compatibility는 유지합니다. 새 env 이름으로 한 번에 바꾸지 않습니다. 먼저 기존 env를 읽어 새 config 구조에 normalize하고, 운영 smoke가 끝난 뒤 새 env alias를 도입합니다.

## infra 결정

`hololive-api`는 PostgreSQL pool과 Valkey client를 process당 한 번만 생성합니다.

현재 3개 runtime이 각각 pool을 만들면 runtime당 max connection이 누적됩니다. 병합 후에도 같은 값을 그대로 합산하면 connection pressure가 유지되고, 반대로 너무 낮추면 request plane과 scheduler plane이 서로 blocking할 수 있습니다.

초기값은 다음으로 둡니다.

```text
POSTGRES_POOL_MIN_CONNS = 2
POSTGRES_POOL_MAX_CONNS = 12
VALKEY_CLIENTS          = 1 shared client + plane-specific namespace wrapper
```

초기 max 12는 기존 bot/admin/llm pool max의 합에 맞춘 안전값입니다. 운영에서 `pg_stat_statements`, `pg_stat_activity`, request latency, scheduler completion latency를 본 뒤 8 또는 10으로 낮춥니다. 병합 PR에서 임의로 낮추지 않습니다.

DB I/O 쪽 NFR은 다음을 지킵니다.

- request-facing route는 scheduler batch query와 같은 transaction을 공유하지 않습니다.
- llm/membernews/majorevent background job은 context timeout을 반드시 갖습니다.
- large digest query는 streaming보다 bounded page query를 우선합니다.
- outbox write는 기존 idempotency key를 유지합니다.
- migration 중 schema 변경은 금지합니다.

## lifecycle 결정

`hololive-api`는 shared `applifecycle.GroupRuntime`으로 logical plane을 기동합니다.

시작 순서와 종료 순서는 의도적으로 분리합니다. GroupRuntime은 declaration order로 start하고 reverse order로 shutdown합니다. 따라서 public listener를 마지막에 등록하면 shutdown 때 가장 먼저 drain됩니다.

권장 등록 순서는 다음입니다.

```go
group := applifecycle.NewGroupRuntime(logger,
    applifecycle.GroupComponent{Name: "llm", Start: llm.Start, Shutdown: llm.Shutdown},
    applifecycle.GroupComponent{Name: "admin", Start: admin.Start, Shutdown: admin.Shutdown},
    applifecycle.GroupComponent{Name: "bot", Start: bot.Start, Shutdown: bot.Shutdown},
)
```

이 경우 shutdown 순서는 `bot -> admin -> llm`입니다. 외부 ingress를 먼저 닫고, 내부 admin plane을 닫고, 마지막으로 scheduler/LLM plane을 정리합니다.

`alarm-worker`와 `youtube-producer`는 이 group에 들어오지 않습니다. 이 둘은 장애 격리와 배포 주기가 다른 runtime입니다.

## alarm HTTP provider 결정

3-runtime 목표에서 `/internal/alarm/*` provider는 `alarm-worker`가 가져갑니다. `hololive-api`는 facade/client만 유지합니다.

이유는 다음과 같습니다.

- alarm domain owner가 scheduler/checker/dispatch/eject path와 같은 process에 있어야 합니다.
- bot/admin 배포가 alarm API provider availability를 흔들면 안 됩니다.
- proactive egress lease와 alarm write path의 상태 판단 기준을 한 runtime 안에 모으는 편이 장애 원인 분석이 쉽습니다.

migration 순서는 다음입니다.

1. `alarm-worker`에 `/internal/alarm/*` route를 등록합니다.
2. `admin-api`의 기존 provider route는 compatibility mode로 남기되 내부적으로 `alarm-worker` client를 호출하게 합니다.
3. `bot` alarm client base URL을 `alarm-worker`로 바꿉니다.
4. 운영 smoke 후 `hololive-api` 안의 compatibility provider를 제거합니다.
5. `CONTRACT_MAP.md`, `CONTRACT_MANIFEST.txt`, `contracts/alarm.md`, service docs, runbooks를 같은 PR 또는 바로 다음 PR에서 갱신합니다.

## logging / IO 결정

`hololive-api`는 3개 file logger를 따로 열지 않습니다. 하나의 process logger를 사용하고 모든 log record에 `plane` 또는 `component` attribute를 붙입니다.

```text
hololive-api.log
  plane=bot
  plane=admin
  plane=llm
```

이 방식은 다음 문제를 피합니다.

- 같은 process에서 여러 lumberjack writer를 열어 rotation timing이 엇갈리는 문제
- stdout/stderr와 file mirror 간 순서가 더 크게 벌어지는 문제
- 장애 시 어떤 process가 어느 file descriptor를 쥐고 있는지 불명확해지는 문제

Docker log는 계속 1차 source입니다. file log는 기존 정책처럼 보조 mirror입니다.

## metrics / pprof 결정

초기 rollout에서는 기존 port compatibility를 유지합니다.

```text
bot metrics   :30091
admin metrics :30096
llm metrics   :30093
bot pprof     :30061
```

최종적으로는 하나의 metrics endpoint로 합칠 수 있지만, 첫 migration PR에서 metrics endpoint를 합치지 않습니다. 운영 dashboard와 alert rule을 동시에 바꾸지 않기 위해서입니다.

새 metric에는 최소한 다음 label을 둡니다.

```text
runtime="hololive-api"
plane="bot|admin|llm"
```

## memory / GC 결정

현재 memory envelope를 단순 합산하면 대략 다음입니다.

```text
bot          512MiB deploy limit / 450MiB GOMEMLIMIT
admin-api    384MiB deploy limit / 320MiB GOMEMLIMIT
llm          320MiB deploy limit / 256MiB GOMEMLIMIT
```

병합 후 초기값은 보수적으로 둡니다.

```text
hololive-api deploy memory limit = 1024MiB
GOMEMLIMIT                         = 768MiB
GOGC                               = 80
```

이 값은 기존 합산보다 낮지만, PostgreSQL pool, Valkey client, common domain cache, HTTP transport를 중복 생성하지 않는다는 전제에서 출발합니다. 운영 관측 없이 512MiB 이하로 낮추지 않습니다.

성능 기준은 다음입니다.

- bot webhook p95 latency가 병합 전 대비 10% 이상 나빠지면 rollback합니다.
- admin dashboard API p95 latency가 10% 이상 나빠지면 rollback합니다.
- llm scheduler completion latency가 20% 이상 나빠지면 scheduler worker count와 DB pool을 먼저 조정하고, 해결되지 않으면 rollback합니다.
- Go heap 목표치가 15분 이상 GOMEMLIMIT의 90%를 넘으면 rollback 또는 memory envelope 상향을 검토합니다.

## failure isolation 결정

`hololive-api` 안에서 plane 하나의 async error가 발생하면 process-level errCh로 전파합니다. 단, 모든 error를 즉시 process fatal로 보지는 않습니다.

- HTTP listener bind 실패: fatal
- shared infra init 실패: fatal
- bot webhook handler init 실패: fatal
- admin router init 실패: fatal
- llm scheduler init 실패: fatal
- individual scheduled job failure: non-fatal, metric/log로 기록
- config subscriber loop 종료: context canceled면 정상, 그 외는 error

panic recovery는 request middleware와 scheduled job boundary에 둡니다. process bootstrap 전체를 recover로 감싸서 장애를 숨기지 않습니다.

## rollout plan

### Phase 0: scaffold

- `applifecycle.GroupRuntime` 추가
- group lifecycle unit test 추가
- 본 design 문서 추가

### Phase 1: alarm provider ownership 정리

- `alarm-worker`에 `/internal/alarm/*` provider 등록
- `admin-api`는 provider가 아니라 facade/client로 전환
- `bot` alarm client URL을 `alarm-worker`로 전환
- queue ownership은 기존과 동일하게 `alarm-worker`가 유지

### Phase 2: `hololive-api` module 생성

- `hololive/hololive-api/go.mod` 생성
- `cmd/hololive-api/main.go` 생성
- shared infra builder 생성
- bot/admin/llm plane runtime shell 생성
- old module의 `internal` package를 import하지 않고 새 module 내부로 move

### Phase 3: compatibility listener rollout

- `:30001`, `:30006`, `:30003` listener 유지
- Docker network alias로 기존 service name 유지
- health/readiness endpoint를 기존 path로 유지
- 기존 `hololive-bot`, `hololive-admin-api`, `llm-scheduler` Compose service는 rollback profile로 남김

### Phase 4: default Compose 전환

- production default에서 `hololive-api` 사용
- old 3 services는 disabled rollback profile로 이동
- deployment script service allowlist 갱신
- log scripts가 `hololive-api --plane` filter를 지원하도록 갱신

### Phase 5: old modules retirement

- 운영 smoke와 한 release window 이후 old modules 삭제
- `go.work`에서 old modules 제거
- runtime split tests를 3-runtime target tests로 교체
- `PROJECT_MAP.md`, `SERVICE_OWNERSHIP.md`, `CONTRACT_MAP.md`를 current SSOT로 갱신

## validation gates

각 phase는 다음 gate를 통과해야 합니다.

```bash
go test ./hololive/hololive-shared/pkg/applifecycle/...
go test ./hololive/hololive-shared/...
go test ./hololive/hololive-api/...
go test ./hololive/hololive-alarm-worker/...
go test ./hololive/hololive-youtube-producer/...
./scripts/architecture/check-project-map.sh
./scripts/architecture/check-runbook-coverage.sh
./scripts/architecture/check-contract-map.sh
./scripts/architecture/ci-boundary-gate.sh
./scripts/deploy/test-compose-services.sh
./scripts/deploy/test-compose-h3-contract.sh
./build-all.sh --no-bump
```

운영 smoke는 최소 다음을 포함합니다.

```text
GET  :30001/health
GET  :30006/health
GET  :30003/health
POST Kakao/Iris webhook smoke
GET  admin dashboard bootstrap API
POST membernews digest command path
POST majorevent trigger path
POST alarm CRUD via alarm-worker provider
claim notification_delivery_outbox dry-run
claim youtube_notification_outbox dry-run
```

## rollback 기준

다음 중 하나라도 발생하면 default Compose를 old services profile로 되돌립니다.

- bot webhook p95 latency 10% 이상 악화
- admin API p95 latency 10% 이상 악화
- llm scheduler job completion latency 20% 이상 악화
- heap pressure가 GOMEMLIMIT 90% 이상으로 15분 이상 유지
- PostgreSQL active connection이 max의 90% 이상으로 10분 이상 유지
- Valkey command latency가 병합 전 대비 20% 이상 악화
- alarm egress 또는 youtube outbox delivery가 중단

Rollback은 schema rollback을 요구하지 않아야 합니다. 이 migration은 runtime topology 변경이며 schema 변경을 포함하지 않습니다.

## acceptance criteria

3-runtime 전환 완료 조건은 다음입니다.

- `docker compose ps` 기준 application runtime이 `hololive-api`, `hololive-alarm-worker`, `youtube-producer` 3개로 정리됩니다.
- `hololive-api`는 1개 Go process입니다.
- old service name은 compatibility alias 또는 deprecated profile 외에는 runtime으로 뜨지 않습니다.
- `hololive-api`는 old module `internal` package를 import하지 않습니다.
- `alarm-worker`가 alarm HTTP provider입니다.
- `youtube-producer`는 기존 active-active AP contract를 유지합니다.
- existing ports `30001`, `30006`, `30003`가 migration window 동안 유지됩니다.
- local CI와 architecture boundary gate가 통과합니다.
