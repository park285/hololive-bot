# OpenBao Runtime Env Minimization

Iris 제외(Hololive Bot 계열 + ChatBotGo Kakao) 운영 ENV 계약 변경의 cross-repo 상위 계획.
ChatBotGo 세부 계약은 `/home/kapu/work/iris-stack/chat-bot-go-kakao/docs/current/architecture/openbao-env-minimization.md`가 소유한다.

## Goal

Hololive Bot 계열과 ChatBotGo Kakao의 영구 repo-local `.env*` 의존을 제거한다. 목표는 완전 file-less 전환이 아니라 OpenBao Agent가 렌더한 `/run/<service>/...` 파일을 운영 source of truth로 삼고, 컨테이너별로 실제 읽는 key만 주입하는 것이다.

## Scope

| Repo / 위치 | 역할 | 호스트 |
|---|---|---|
| `hololive-bot/` | compose 계약, deploy 스크립트, repo security contract test | central(100.100.1.3) + Osaka AP + Seoul AP |
| `chat-bot-go-kakao/` | compose 계약, config 로더, redeploy 스크립트 | central(100.100.1.3) |
| `/home/kapu/work/openbao-secrets-stack/` | agent 템플릿, AppRole policy, systemd unit, KV 경로 | OpenBao 서버 + 각 렌더 호스트 |

관련 문서: `openbao-secrets-stack/docs/08-current-env-centralization.md`(중앙화 1차 결과), `docs/05-systemd-agent-rendering.md`(agent unit 계약).

## Non-Goals

- Iris runtime env 변경.
- 앱 시작 시 OpenBao API를 직접 조회하는 구조.
- Postgres/Valkey official image bootstrap secret 제거.
- Compose interpolation 채널 자체의 폐지 — `${VAR:?required}` 보간은 유지한다(D2 참고).

live OpenBao KV write, Agent restart, container recreate, deploy는 non-goal이 아니라 **Phase 3 이후의 approval-gated 작업**이다. Phase 0–2는 read-only 조사와 repo-side 계약 변경만 수행한다.

## Baseline State (2026-06-06 검증, Phase 0-2 실행 전)

Hololive Bot:

- `kv/prod/hololive-bot/env` 단일 KV가 70+ key 모놀리식 `/run/hololive-bot/env`로 렌더된다 (`openbao-secrets-stack/config/agent-hololive-bot.hcl:26-106`). H3 서버 cert/key는 PKI 발급(`pki/issue/hololive-bot-h3-server`, hcl:109-132), iris CA는 `kv/prod/hololive-bot/certs`에서 파일 렌더(hcl:134-144).
- `docker-compose.prod.yml`: egress 2종(`hololive-bot`:334, `hololive-alarm-worker`:434)만 `${COMPOSE_ENV_FILE:-/run/hololive-bot/env}`를 broad `env_file`로 받는다. non-egress 4종(`hololive-admin-api`, `youtube-producer`, `llm-scheduler`, `admin-dashboard`)은 `env_file` 없이 명시 `environment:` 보간만 받는다 — per-container 최소화는 이미 절반 달성된 상태.
- **목표 위반 현행 지점**: AP overlay가 youtube-producer 인스턴스에 모놀리식 env를 주입한다 (`docker-compose.osaka.yml:31-32,58-59`, `docker-compose.seoul.yml:27-28,57-58`). AP 인스턴스가 Iris egress token 포함 전체 key를 수신 중이다.
- `docker-compose.live-compat.yml:35-36,63-64`: egress 2종의 broad `env_file` 복원을 명시한다.
- `scripts/deploy/compose.sh:113-128`: `COMPOSE_ENV_FILE` resolve → 형식 검증 → `--env-file` 주입. 기본값은 `/run/hololive-bot/env` (`scripts/deploy/lib/compose-env.sh:13`, `OPENBAO_HOLOLIVE_ENV_FILE`).
- `repo_security_contract_test.go` 현행 단정은 **현행 모놀리식 계약을 강제**한다: egress 2종 `env_file` 필수(:111), non-egress `env_file` 금지(:122), 렌더 결과 token 부재(:157), live-compat의 `env_file` 복원 필수(:247), AP 파일에 literal `- /run/hololive-bot/env` 요구(:565).
- **baseline red**: `TestRepoComposeAPCertMountsAreMinimized` osaka/seoul이 HEAD에서 실패한다 — overlay는 `${COMPOSE_ENV_FILE:-/run/hololive-bot/env}` 형태인데 테스트(:565)는 literal `- /run/hololive-bot/env`를 기대. 사전 결함이며 Phase 2 테스트 재작성에서 흡수한다.

ChatBotGo:

- **Agent 템플릿 분할은 이미 완료**: `common.env`/`bot.env`/`memory-service.env`/`kimi.env` + PKI cert 렌더 (`config/agent-chatbotgo.hcl`). 남은 작업은 compose·코드·스크립트 계약 전환이다.
- `deploy/compose/chatbotgo.yml:15-17`: repo-local `../../.env`, `../../.env.chatbotgo` 사용. `:46`: broad `../../secrets:/run/secrets:ro` mount(내용물은 cert 3종 + `.bak` — 코드가 `/run/secrets`를 읽는 경로는 없음).
- `internal/config/load.go:67`: working directory `.env` 무조건 자동 로드.
- `scripts/chatbotgo-redeploy.sh:298`: `$ROOT_DIR/.env.chatbotgo` 전제 검증.
- `openbao-secrets-stack/deploy/compose/docker-compose.chatbotgo.openbao.yml`: `/run/chatbotgo/*.env`만 읽는 full-cutover compose가 **이미 별도 존재** — SSOT 수렴 대상(D4).

OpenBao stack:

- `config/agent-hololive-bot.hcl`, `config/agent-chatbotgo.hcl`, `policies/prod-hololive-bot-read.hcl`, `policies/prod-chatbotgo-read.hcl` 모두 기존 파일이다. Phase 3는 신규 생성이 아니라 **기존 템플릿 수정**이다.
- 두 policy 모두 `kv/data/prod/<svc>/*` 전체 read — 호스트 단위 분리는 없다. Osaka/Seoul AP 호스트도 동일 AppRole로 전체 hololive-bot KV를 읽을 수 있다(D1).

## Target State

Hololive Bot:

- 운영 checkout 아래 `.env`, `.env.osaka`, `.env.seoul` 같은 secret 파일이 필요 없다.
- `COMPOSE_ENV_FILE`은 Compose interpolation 입력으로만 사용하고, 어떤 컨테이너의 `env_file:`로도 쓰지 않는다.
- app container는 monolithic `/run/hololive-bot/env`를 broad `env_file`로 받지 않는다. 앱 전용 key는 per-service env 파일로 받는다.
- `hololive-bot`과 `hololive-alarm-worker`만 Iris egress token을 받는다(`*iris-env` 보간 유지).
- `youtube-producer`(AP a/b/c 포함), `llm-scheduler`, `hololive-admin-api`, `admin-dashboard`는 rendered config 기준 `IRIS_WEBHOOK_TOKEN`/`IRIS_BOT_TOKEN`이 존재하지 않는다.

ChatBotGo:

- 운영 checkout 아래 `.env`와 `.env.chatbotgo`가 필요 없다.
- `bot-chatbotgo`는 `/run/chatbotgo/common.env` + `/run/chatbotgo/bot.env`를 사용한다.
- 앱의 `.env` 자동 로드는 명시 opt-in으로 제한한다(`CHATBOTGO_LOAD_DOTENV` 계약 — 세부는 companion 문서).
- broad `../../secrets` mount는 제거한다. cert는 `/run/chatbotgo/certs:ro`로 충분하다.
- `deploy/compose/infra.yml`의 `bot` 서비스(local/dev 경로)는 이번 변경의 운영 대상이 아니다 — local opt-in 경계로 명시하고 유지한다.

채널 정의(혼동 방지):

1. **Compose interpolation 입력**(`compose.env`): `${VAR}`/`${VAR:?}` 보간용. 보간은 각 서비스 `environment:` 블록이 참조한 key만 컨테이너에 들어가므로 per-container 최소화와 양립한다. 단 `DB_PASSWORD`, `CACHE_PASSWORD`, `IRIS_*_TOKEN`, `ADMIN_PASS_BCRYPT`, `SESSION_SECRET`, `API_SECRET_KEY` 등 **secret이 계속 포함되는 파일**이다. "compose.env = non-secret"이 아님을 전제로 perms 0600 유지.
2. **AP Compose interpolation 입력**(`ap-compose.env`): Osaka/Seoul AP host 전용 보간 파일이다. AP producer에 필요한 DB/cache/API/H3/Iris endpoint key는 유지하지만 `IRIS_WEBHOOK_TOKEN`/`IRIS_BOT_TOKEN`은 넣지 않는다.
3. **Per-service env_file**: compose `environment:` 블록에 없는 앱 전용 key만 담는다 (예: bot의 `KAKAO_REST_API_KEY`/`NAVER_*`/`KASI_*`/`TOKEN_ENCRYPTION_KEY`, alarm-worker의 `ALARM_HOLODEX_API_KEYS`/`HOLOLIVE_ALARM_*`, youtube-producer의 `HOLODEX_API_KEY_2..5`/`SCRAPER_PROXY_*`/`YOUTUBE_*` 계열).

key→파일 배치는 Phase 0의 ownership matrix가 단일 근거다. matrix에 없는 key는 어떤 렌더 파일에도 넣지 않는다.

## Render Shape (현행 대비 표기)

```text
# KV (기존)
kv/prod/hololive-bot/env            # 모놀리식 — 전환 기간 병행 유지, Phase 5에서 폐기(D5)
kv/prod/hololive-bot/certs          # IRIS_H3_CA_CERT — 유지
kv/prod/chatbotgo/common-env        # 유지
kv/prod/chatbotgo/bot-env           # 유지
kv/prod/chatbotgo/memory-service-env # 소비자 확인 후 정리 후보(D3)
kv/prod/chatbotgo/kimi-env          # 소비자 확인 후 정리 후보(D3)

# KV (신규 — Phase 3 write, approval-gated)
kv/prod/hololive-bot/compose-env
kv/prod/hololive-bot/ap-compose-env
kv/prod/hololive-bot/bot-env
kv/prod/hololive-bot/alarm-worker-env
kv/prod/hololive-bot/youtube-producer-env
```

```text
# 렌더 결과 (호스트별)
central:  /run/hololive-bot/compose.env  bot.env  alarm-worker.env  youtube-producer.env  certs/*
Osaka AP: /run/hololive-bot/ap-compose.env  youtube-producer.env  certs/*        # egress env/token 불필요
Seoul AP: /run/hololive-bot/ap-compose.env  youtube-producer.env  certs/*        # egress env/token 불필요
central:  /run/chatbotgo/common.env  bot.env  certs/*                          # 기존 렌더 그대로
```

cert는 현행 유지: H3 서버 cert/key는 PKI 발급(`pki/issue/<svc>-h3-server`), CA는 KV(`hololive-bot`) 또는 `pki/cert/ca`(`chatbotgo`). `kv/prod/chatbotgo/certs` 같은 신규 KV cert 경로는 만들지 않는다.

Rules:

- `compose.env`/`ap-compose.env`에는 OpenBao source에 실제 존재하는 compose 보간 key만 둔다. `${VAR:-default}`로 충분한 optional/default tuning key는 합성하지 않는다.
- per-service env에는 해당 service가 실제로 읽는 key만 둔다(ownership matrix 근거).
- certificate/private key/multiline 값은 env가 아니라 file render로 유지한다.
- OpenBao Agent template key list는 정적이다 — key 추가 시 KV·template·matrix·계약 테스트를 한 commit 단위로 같이 갱신한다.

## Open Decisions

| ID | 결정 사항 | 권고 |
|---|---|---|
| D1 | AP 호스트 AppRole/policy 분리(AP는 `youtube-producer-env`+`ap-compose-env`+PKI만 read) | 분리 권장 — AP 호스트 탈취 시 egress token 노출 차단. AP compose input도 `ap-compose.env`로 분리해 token-free로 유지한다. |
| D2 | `${VAR:?}` interpolation secret을 per-service env_file로 이전할지 | 1차에서는 유지. 보간 채널 폐지는 compose 전반 재설계라 범위 초과 |
| D3 | `memory-service.env`/`kimi.env` 렌더 및 KV 정리 | Phase 0에서 소비자 부재 확인 → Phase 5에서 template 제거 + KV 보존 스냅샷 후 폐기 |
| D4 | `openbao-secrets-stack/deploy/compose/docker-compose.chatbotgo.openbao.yml` 처리 | chat-bot-go-kakao 본가 compose로 수렴하고 해당 파일은 폐기(superseded 표기) |
| D5 | 모놀리식 `kv/prod/hololive-bot/env` 폐기 시점 | Phase 4 완료 + 7일 관찰 후 Phase 5에서 template 제거 → KV 폐기 |

## Execution Phases

### Phase 0: Read-Only Inventory + Key Ownership Matrix

No live write, no restart. 산출물: **key→{compose.env, bot.env, alarm-worker.env, youtube-producer.env, 제거}** 매핑 표(이 문서에 추가 commit).

```bash
# hololive-bot repo root에서
rg -n "env_file|COMPOSE_ENV_FILE|/run/hololive-bot/env|\.env" deploy scripts docs hololive admin-dashboard -S

# compose interpolation key 전수 도출 (= compose.env 후보)
. scripts/deploy/lib/compose-env.sh
compose_env_list_interpolation_keys_from_files deploy/compose/*.yml

# 각 렌더 호스트(central / Osaka / Seoul)에서 — 값 미출력, key 이름만
sudo find /run/hololive-bot /run/chatbotgo -maxdepth 3 -printf "%M %u %g %s %p\n" | sort
sudo sh -c 'for f in /run/hololive-bot/*.env /run/hololive-bot/env /run/chatbotgo/*.env; do test -r "$f" && printf "%s\n" "$f" && cut -d= -f1 "$f" | sort; done'

# 호스트 인벤토리: 어떤 호스트가 어떤 agent unit을 돌리는지 고정
systemctl list-units 'openbao-agent-*' --no-pager
```

추가 확인 항목:

- 모놀리식 env의 각 key를 읽는 서비스 식별(`rg`로 settings/config 패키지 추적) → matrix 완성.
- `memory-service.env`/`kimi.env` 소비자 존재 여부(D3 입력).
- AP 호스트별 PKI `ip_sans` 적합성(현행 hcl은 `ip_sans=100.100.1.3` 고정 — AP 호스트 렌더 시 올바른 SAN인지).

#### Phase 0 Result (2026-06-06 repo-side)

Read-only inventory evidence:

- `compose_env_list_interpolation_keys_from_files deploy/compose/*.yml`로 compose 보간 key를 도출했다. Phase 2 repo-side 계약 플립 뒤 기준이며, `HOLOLIVE_*_ENV_FILE` 같은 경로 override key는 렌더 파일 대상이 아니라 shell control key로 분리한다.
- `/home/kapu/work/openbao-secrets-stack/config/agent-hololive-bot.hcl`은 key 이름만 읽었다. 값은 조회하거나 출력하지 않았다.
- local `/run/hololive-bot/env`는 존재하지만 현재 사용자에게 key 이름을 읽을 권한이 없다(`0600 root:kapu`). `/run/hololive-bot/{compose,bot,alarm-worker,youtube-producer}.env`는 Phase 3 전이라 존재하지 않는다. AP 호스트 `/run/*`와 PKI SAN 적합성은 live host 조회가 필요한 gap으로 남긴다.
- local `/run/chatbotgo/{common,bot,memory-service,kimi}.env`는 key 이름만 읽을 수 있었다. `memory-service.env`와 `kimi.env`는 현 생산 compose 소비자가 확인되지 않아 D3/Phase 5 정리 후보로 유지한다.

Ownership matrix rule: 아래 목록에 없는 key는 Phase 3 template/KV 분배에 추가하지 않는다. 새 key가 필요해지면 이 matrix와 계약 테스트를 먼저 갱신한다.

| Owner | Keys / policy |
|---|---|
| `compose.env` | central host compose `environment:` 보간 소유. 아래 `compose.env keys`가 실제 OpenBao-rendered bundle이다. |
| `ap-compose.env` | Osaka/Seoul AP host compose 보간 소유. `compose.env`에서 Iris egress token만 제외한 token-free AP bundle이다. |
| `bot.env` | `hololive-bot` 앱 전용 key. compose `environment:`가 이미 주입하는 DB/Cache/Iris/H3/API/Kakao key는 넣지 않는다. |
| `alarm-worker.env` | `hololive-alarm-worker` 앱 전용 key. egress 공통 key와 `config.Load` 필수 key는 compose `environment:`로 유지한다. |
| `youtube-producer.env` | AP `youtube-producer` 전용 key. `IRIS_WEBHOOK_TOKEN`/`IRIS_BOT_TOKEN`은 절대 넣지 않는다. |
| `제거` | repo reader가 없거나 compose가 literal/generated env로 직접 만드는 alias, shell control override, legacy checkout-only key. Certificate material처럼 env가 아닌 파일 렌더로 유지되는 항목은 env matrix에서만 제외한다. 실제 KV 폐기는 Phase 5에서 보존 스냅샷 뒤 별도 판단한다. |

`compose.env keys`:

```text
ADMIN_ALLOWED_IPS
ADMIN_PASS_BCRYPT
ADMIN_USER
ALARM_TWITCH_ENABLED
API_SECRET_KEY
CACHE_PASSWORD
CLIPROXY_API_KEY
CLIPROXY_BASE_URL
CLIPROXY_MODEL
CLIPROXY_REASONING_EFFORT
DB_PASSWORD
HOLODEX_API_KEY
HOLODEX_API_KEY_1
HOLOLIVE_BOT_PORT_BIND_IP
HOLOLIVE_DB_USER
HOLOLIVE_H3_ADDR
HOLOLIVE_H3_CERT_FILE
HOLOLIVE_H3_KEY_FILE
HOLOLIVE_H3_SERVER_NAME
HOLOLIVE_HTTP_TRANSPORTS
HOLOLIVE_INTERNAL_H3_SERVER_NAME
HOLOLIVE_MIGRATOR_USER
HOLOLIVE_SCRAPER_PASSWORD
HOLOLIVE_SCRAPER_USER
HOLO_BOT_API_KEY
IRIS_BASE_URL
IRIS_BOT_TOKEN
IRIS_H3_CA_CERT_FILE
IRIS_H3_SERVER_NAME
IRIS_TRANSPORT
IRIS_WEBHOOK_TOKEN
LOG_LEVEL
MAJOREVENT_SCRAPER_ENABLED
POSTGRES_ADMIN_USER
POSTGRES_SSLMODE
SESSION_SECRET
VALKEY_PORT_BIND_IP
YOUTUBE_API_KEY
```

`ap-compose.env keys`:

```text
ADMIN_ALLOWED_IPS
ADMIN_PASS_BCRYPT
ADMIN_USER
ALARM_TWITCH_ENABLED
API_SECRET_KEY
CACHE_PASSWORD
CLIPROXY_API_KEY
CLIPROXY_BASE_URL
CLIPROXY_MODEL
CLIPROXY_REASONING_EFFORT
DB_PASSWORD
HOLODEX_API_KEY
HOLODEX_API_KEY_1
HOLOLIVE_BOT_PORT_BIND_IP
HOLOLIVE_DB_USER
HOLOLIVE_H3_ADDR
HOLOLIVE_H3_CERT_FILE
HOLOLIVE_H3_KEY_FILE
HOLOLIVE_H3_SERVER_NAME
HOLOLIVE_HTTP_TRANSPORTS
HOLOLIVE_INTERNAL_H3_SERVER_NAME
HOLOLIVE_MIGRATOR_USER
HOLOLIVE_SCRAPER_PASSWORD
HOLOLIVE_SCRAPER_USER
HOLO_BOT_API_KEY
IRIS_BASE_URL
IRIS_H3_CA_CERT_FILE
IRIS_H3_SERVER_NAME
IRIS_TRANSPORT
LOG_LEVEL
MAJOREVENT_SCRAPER_ENABLED
POSTGRES_ADMIN_USER
POSTGRES_SSLMODE
SESSION_SECRET
VALKEY_PORT_BIND_IP
YOUTUBE_API_KEY
```

`bot.env keys`:

```text
(none; file remains as an empty env_file target so Compose wiring stays stable)
```

`alarm-worker.env keys`:

```text
CELEBRATION_CHECK_HOUR_KST
CELEBRATION_RUN_INTERVAL_MS
CELEBRATION_RUNNER_ENABLED
TWITCH_CLIENT_ID
TWITCH_CLIENT_SECRET
```

`youtube-producer.env keys`:

```text
HOLODEX_API_KEY_2
HOLODEX_API_KEY_3
HOLODEX_API_KEY_4
HOLODEX_API_KEY_5
SCRAPER_PROXY_ENABLED
SCRAPER_PROXY_URL
YOUTUBE_COMMUNITY_SHORTS_BIGBANG_CUTOVER_AT
YOUTUBE_ENABLE_QUOTA_BUILDING
```

`제거 / 렌더 제외 keys`:

```text
AIRKOREA_API_KEY
ALARM_HOLODEX_API_KEYS
APP_ENV
BOT_CALENDAR_ENTRY_CACHE_TTL_SECONDS
BOT_CALENDAR_IMAGE_CACHE_DIR
BOT_MENTION_PREFIX
BOT_PREFIX
BOT_SELF_USER
CACHE_HOST
CACHE_PORT
CHECK_INTERVAL_SECONDS
CHZZK_CLIENT_ID
CHZZK_CLIENT_SECRET
CLIPROXY_ENABLED
COMPOSE_ENV_FILE
COMPOSE_PROFILES
COMPOSE_PROJECT_NAME
GOOGLE_CLIENT_ID
GOOGLE_CLIENT_SECRET
GOOGLE_REDIRECT_URI
HOLO_ADMIN_API_VERSION
HOLO_ALARM_WORKER_VERSION
HOLO_BOT_VERSION
HOLOLIVE_ALARM_PASSWORD
HOLOLIVE_ALARM_USER
HOLOLIVE_ALARM_WORKER_ENV_FILE
HOLOLIVE_BOT_ENV_FILE
HOLOLIVE_YOUTUBE_PRODUCER_ENV_FILE
IRIS_CLIENT_GO_WORKSPACE_PATH
IRIS_H3_CA_CERT
IRIS_SHARED_TOKEN
KAKAO_REST_API_KEY
KASI_API_KEY
KMA_API_KEY
NAVER_CLIENT_ID
NAVER_CLIENT_SECRET
NOTIFICATION_ADVANCE_MINUTES
OAUTH_BASE_URL
OPENBAO_HOLOLIVE_ENV_FILE
POSTGRES_ADMIN_PASSWORD
POSTGRES_PASSWORD
REMOTE_CACHE_PREFIX
SHARED_GO_WORKSPACE_PATH
TOKEN_ENCRYPTION_KEY
YOUTUBE_PRODUCER_ACTIVE_ACTIVE_INSTANCE_COUNT
YOUTUBE_PRODUCER_BUDGET_ACQUIRE_TIMEOUT_MS
YOUTUBE_PRODUCER_BUDGET_BACKFILL_MAX_INFLIGHT
YOUTUBE_PRODUCER_BUDGET_BROWSER_SNAPSHOT_MAX_INFLIGHT
YOUTUBE_PRODUCER_BUDGET_FALLBACK_MAX_INFLIGHT
YOUTUBE_PRODUCER_BUDGET_HOLODEX_LIVE_MAX_INFLIGHT
YOUTUBE_PRODUCER_BUDGET_WINDOW_CHECK_ENABLED
YOUTUBE_PRODUCER_BUDGET_YOUTUBE_SCRAPER_MAX_INFLIGHT
YOUTUBE_PRODUCER_GLOBAL_BUDGET_ENABLED
```

### Phase 1: ChatBotGo Contract

세부 계약·작업 목록·검증은 companion 문서가 소유한다. 이 계획에서의 추가 요구:

- compose interpolation key(`CHATBOTGO_UID/GID`, `LOG_*`, `CHATBOTGO_H3_*`)가 전부 안전한 기본값을 갖는지 확인 — 충족 시 chatbotgo용 `compose.env`는 만들지 않는다.
- `infra.yml`의 `bot` 서비스(`.env` + `secrets` mount)는 local 전용 경계로 문서화하고 운영 계약에서 제외.
- D4 수렴: 본가 `deploy/compose/chatbotgo.yml`이 `/run/chatbotgo/*.env`를 읽도록 바뀐 뒤 openbao-stack 쪽 중복 compose를 superseded 처리.

### Phase 2: Hololive Bot Contract

Repo-side 변경만. **기본 경로 플립은 merge 가능하지만, 대상 호스트에 Phase 3 렌더가 생기기 전까지 deploy 금지** — 전환 기간에는 `COMPOSE_ENV_FILE`/명시 경로로 구버전 호환을 유지한다.

대상 파일:

- `deploy/compose/docker-compose.prod.yml` — egress 2종 `env_file`을 per-service 파일(`/run/hololive-bot/bot.env`, `alarm-worker.env`)로 교체.
- `deploy/compose/docker-compose.osaka.yml`, `docker-compose.seoul.yml` — AP youtube-producer `env_file`을 `/run/hololive-bot/youtube-producer.env`로 교체(모놀리식 주입 제거 = Iris token 차단).
- `deploy/compose/docker-compose.live-compat.yml` — broad env 복원 로직을 per-service 파일 기준으로 재작성.
- `deploy/compose/docker-compose.main-ap.yml`, `docker-compose.main-ap.live-compat.yml` — youtube-producer-c가 `env_file` 없이 유지되는지 계약 검증 대상에 포함.
- `deploy/compose/README.md` — 운영 입력 계약 서술 갱신.
- `scripts/deploy/lib/compose-env.sh` — 기본값을 `/run/hololive-bot/compose.env`로 플립(`OPENBAO_HOLOLIVE_ENV_FILE` 변수명 유지 여부 포함).
- `scripts/deploy/compose.sh` — `--env-file` 주입 로직은 유지, resolve 기본값만 변경.
- `scripts/deploy/test-compose-env.sh`, `test-compose-h3-contract.sh` — 스텁 env 경로 갱신.
- `hololive/hololive-shared/pkg/config/internal/settings/repo_security_contract_test.go` — **계약 플립 명세**:
  - :111 "egress 2종 모놀리식 `env_file` 필수" → "egress 2종 per-service `env_file` 필수, 모놀리식 경로 금지"로 반전.
  - :247 "live-compat가 broad env 복원" → per-service 기준으로 반전.
  - :565 literal `- /run/hololive-bot/env` 요구 → 신규 경로 기준 재작성(현행 baseline red도 함께 해소).
  - 유지: non-egress `env_file` 금지(:122), 렌더 후 IRIS token 부재(:157) — youtube-producer AP 렌더에 대한 token 부재 단정을 **추가**.

#### Phase 2 Repo-Side Status (2026-06-06)

- `docker-compose.prod.yml`과 `docker-compose.live-compat.yml`의 egress `env_file` 기본값은 각각 `/run/hololive-bot/bot.env`, `/run/hololive-bot/alarm-worker.env`로 전환했다.
- `hololive-bot`/`hololive-alarm-worker`는 `KAKAO_*`, `API_SECRET_KEY`, `HOLODEX_API_KEY(_1)`, `YOUTUBE_API_KEY`를 `compose.env` 보간 기반 명시 `environment:`로 받는다. per-service `env_file`에는 앱 전용 key만 남긴다.
- Osaka/Seoul AP overlay의 `youtube-producer` `env_file` 기본값은 `/run/hololive-bot/youtube-producer.env`로 전환했다. AP producer 렌더 결과에 `IRIS_WEBHOOK_TOKEN`/`IRIS_BOT_TOKEN`이 없어야 한다는 계약 테스트가 추가됐다.
- `scripts/deploy/lib/compose-env.sh`의 OpenBao compose 입력 기본값은 `/run/hololive-bot/compose.env`다. `COMPOSE_ENV_FILE`과 `HOLOLIVE_*_ENV_FILE` override는 repo-side 검증/전환 호환용으로 유지한다.
- Phase 3 렌더 파일이 대상 호스트에 생기기 전까지 live deploy/recreate는 금지한다.

### Phase 3: OpenBao Template / Policy Split

신규 KV write, agent config 수정, agent restart — **각 항목 explicit approval 필요**. 기존 파일 수정이다(신규 생성 아님):

- `config/agent-hololive-bot.hcl` — 모놀리식 template을 `compose.env` + per-service template으로 분할. **전환 기간 동안 기존 `/run/hololive-bot/env` template은 병행 유지**(rollback을 compose 되돌리기만으로 가능하게).
- `policies/prod-hololive-bot-read.hcl` — D1 채택 시 host-scope policy 분리, 미채택 시 변경 없음.
- 호스트별 적용 매트릭스: central → Osaka → Seoul 순서로 한 호스트씩. AP 호스트는 token-free `ap-compose.env`+`youtube-producer.env`만 렌더.
- KV 신규 경로 write는 기존 `kv/prod/hololive-bot/env` 값의 분배 복사다 — 값 신규 발급 없음, 회전은 범위 외.
- `config/agent-chatbotgo.hcl` — 변경 없음(이미 분할 완료). D3 정리는 Phase 5로 이연.

#### Phase 3 Repo/Live Status (2026-06-06)

- `openbao-secrets-stack/config/agent-hololive-bot.hcl`은 legacy `/run/hololive-bot/env`를 유지하면서 `/run/hololive-bot/{compose,ap-compose,bot,alarm-worker,youtube-producer}.env` split render template을 추가했다.
- `openbao-secrets-stack/config/agent-hololive-bot-ap.hcl`, `policies/prod-hololive-bot-ap-read.hcl`, `deploy/systemd/openbao-agent-hololive-bot-ap.service`는 AP host 전용이다. AP host는 `/run/hololive-bot/ap-compose.env`, `/run/hololive-bot/youtube-producer.env`, cert 파일만 렌더/read하고 central `/run/hololive-bot/compose.env` 및 legacy `/run/hololive-bot/env`는 렌더하지 않는다.
- AP unit은 `Group=opc`로 실행한다. AP producer image가 `1000:1000` non-root로 실행되므로 cert/key는 `root:opc 0640`으로 렌더되어 container-readable하고, env 파일은 `0600`으로 유지된다.
- `openbao-secrets-stack/scripts/split-hololive-env-bundles.py`는 기존 `kv/prod/hololive-bot/env`를 새 KV bundle 5종으로 분배하는 helper다. `ap-compose-env`는 `IRIS_WEBHOOK_TOKEN`/`IRIS_BOT_TOKEN`을 제외한다. fixture dry-run은 `--source-json`, live KV read dry-run은 `--read-live`를 명시한다. live KV write는 `--write`가 필요하다.
- 기존 monolithic KV에 없는 optional/default key는 합성하지 않는다. `bot.env`는 comment-only 파일로 렌더되고, 앱 기본값을 사용한다.
- `openbao-secrets-stack/scripts/verify-hololive-h3-contract.sh`는 split template 존재, strict missing-key 설정, helper/template key parity, source 누락 key 거부, `ap-compose.env`/AP producer env의 Iris egress token 부재, AP agent/policy의 central env read/render 금지, central/AP systemd `ExecStart=/usr/bin/bao`, AP runtime legacy env 부재를 검증한다.
- live 적용 완료: 신규 KV path write, central installed hcl/verifier 교체, central `openbao-agent-hololive-bot.service` restart, `hololive-bot`/`hololive-alarm-worker` recreate, Osaka/Seoul AP hcl/unit/verifier/AppRole credential 설치, AP agent restart, AP producer recreate.
- AP host H3/QUIC 안정성 계약: `scripts/deploy/ap-iris-h3-trust-preflight.sh`와 `scripts/deploy/ap-completion-check.sh`는 `net.core.rmem_max`/`net.core.wmem_max >= 7500000`을 요구한다. quic-go가 요구하는 UDP socket buffer를 host kernel limit이 막으면 health/preflight는 성공해도 receive buffer warning이 발생하므로, AP host는 `/etc/sysctl.d/99-hololive-quic-udp-buffer.conf` 등으로 값을 영구 적용해야 한다.

Required evidence (호스트별 수집, 값 미출력):

```bash
sudo find /run/hololive-bot /run/chatbotgo -maxdepth 3 -printf "%M %u %g %s %TY-%Tm-%TdT%TH:%TM:%TS %p\n" | sort
journalctl -u openbao-agent-hololive-bot.service -u openbao-agent-chatbotgo.service -n 160 --no-pager |
  grep -E 'rendered|permission denied|x509|no such file|ERROR|WARN' || true
sudo sh -c 'cut -d= -f1 /run/hololive-bot/compose.env /run/hololive-bot/ap-compose.env | sort -u'   # key 이름 대조용
```

### Phase 4: Live Rollout

대상 서비스 집합별 explicit approval 필요.

순서:

1. ChatBotGo: 렌더 확인 → `docker compose -f deploy/compose/chatbotgo.yml config --quiet` → `bot-chatbotgo` recreate → `/health`, `/ready`. 2026-06-06 완료.
2. Hololive central: `hololive-bot`, `hololive-alarm-worker` recreate → health/readiness + Iris egress 경로 스모크. 2026-06-06 완료.
3. Hololive AP: Osaka → Seoul 한 호스트씩. recreate 후 **token 부재 확인**. 2026-06-06 완료:

```bash
# 값 미출력 — key 이름만 검사
docker exec hololive-youtube-producer-a sh -c 'env | cut -d= -f1 | grep -E "^IRIS_(WEBHOOK|BOT)_TOKEN$"' && echo VIOLATION || echo OK
```

Rollback(각 단계 공통): compose 변경 commit revert → 직전 이미지/compose로 recreate. 모놀리식 `/run/hololive-bot/env`와 chatbotgo 구 `.env*`는 Phase 5 전까지 보존되므로 추가 secret 복사 없이 되돌릴 수 있다.

### Phase 5: Cleanup (approval-gated, Phase 4 안정화 이후)

- `agent-hololive-bot.hcl`에서 모놀리식 template 제거 → `kv/prod/hololive-bot/env` 폐기(D5; 사전 Raft snapshot).
- D3 확정 시 `memory-service.env`/`kimi.env` template 제거 + KV 정리.
- chat-bot-go-kakao 운영 checkout의 `.env`, `.env.chatbotgo`, `secrets/` 제거(파괴적 — 별도 승인).
- `openbao-secrets-stack`의 chatbotgo full-cutover compose superseded 처리(D4).
- 관련 문서 갱신: 본 문서, companion, `08-current-env-centralization.md`.

## Stop Rules

- OpenBao Agent login 실패 또는 렌더 파일 누락.
- Agent journal에 `permission denied`/`x509`/render 실패.
- template이 참조하는 KV key 부재(`error_on_missing_key` 포함) — 신규 경로 분배 누락 신호.
- `docker compose config --quiet` 실패.
- non-egress service(youtube-producer AP 포함)가 Iris egress token을 받는 상태 발견.
- ownership matrix에 없는 key가 렌더 파일에 필요해지는 경우 — matrix 갱신 없이 진행 금지.
- 양 repo의 contract test red(사전 합의된 baseline red 항목 제외).
- 어떤 검증이든 raw secret 값을 출력하게 되는 경우.
- health/readiness 실패 after recreate.
- rollback 경로(보존된 모놀리식 렌더/구 compose commit) 부재.

## Verification

Hololive local contract:

```bash
go test ./hololive/hololive-shared/pkg/config/internal/settings
./scripts/deploy/test-compose-env.sh
./scripts/deploy/test-compose-h3-contract.sh
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml config --quiet
COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f deploy/compose/docker-compose.osaka.yml config --quiet
COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f deploy/compose/docker-compose.seoul.yml config --quiet
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f deploy/compose/docker-compose.live-compat.yml config --quiet
```

Token 부재(렌더 시점, 값 미출력):

```bash
COMPOSE_PROFILES=oracle ./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml -f deploy/compose/docker-compose.osaka.yml config --format json |
  python3 -c 'import json,sys; s=json.load(sys.stdin)["services"]; bad=[n for n,v in s.items() if n.startswith("youtube-producer") and any(k in (v.get("environment") or {}) for k in ("IRIS_WEBHOOK_TOKEN","IRIS_BOT_TOKEN"))]; print("OK" if not bad else "VIOLATION: "+",".join(bad)); sys.exit(1 if bad else 0)'
```

ChatBotGo local contract: companion 문서의 Verification 절을 따른다.

Acceptance:

- Hololive Bot·ChatBotGo의 어떤 production 경로도 repo-local `.env*`를 요구하지 않는다.
- 어떤 app container도 monolithic `/run/hololive-bot/env`를 `env_file`로 받지 않는다(AP overlay 포함).
- non-egress 서비스(youtube-producer a/b/c, llm-scheduler, hololive-admin-api, admin-dashboard)의 rendered env에 `IRIS_WEBHOOK_TOKEN`/`IRIS_BOT_TOKEN`이 없다 — config 렌더와 컨테이너 runtime 양쪽에서 key-name-only로 검증.
- OpenBao 렌더 key 목록이 service-scoped이고 ownership matrix와 일치하며, 값 노출 없이 검증됐다.
- 전환 기간 rollback 경로(모놀리식 렌더 병행 + compose revert)가 살아 있고, Phase 5 폐기는 안정화 관찰 후 별도 승인으로 진행한다.
- 양 repo contract test green, 사전 baseline red(:565 계열)는 Phase 2에서 해소됨.
