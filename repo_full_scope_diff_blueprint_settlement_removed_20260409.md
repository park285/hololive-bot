# Full-Scope Additional Diff Blueprint (Settlement Removal Assumed)

## 전제

이번 문서는 최신 업로드본(`hololive-bot-full-20260409T164051Z.tar.gz`) 기준의 **추가 잔존 이슈**만 다룹니다.  
이전 단계에서 이미 해결된 항목(유튜브 알람 exact-minute 미스, aligned scheduler, scraper poll env 반영, worker full 시 임의 `+10s` 지연 제거, bounded lookback 등)은 반복하지 않습니다.

가장 중요한 전제는 다음입니다.

1. `settlement-go`는 **개선 대상이 아니라 제거 대상**입니다.
2. 따라서 settlement 관련 항목은 “정리/표준화”가 아니라 **삭제 + 회귀 방지 + 데이터 퇴역(runbook)** 기준으로 다룹니다.
3. 이번 문서는 **repo-wide** 범위입니다. Go 런타임, `hololive-shared`, `shared-go`, admin-dashboard, docs, Docker build context, migration, CI/architecture gate까지 포함합니다.

---

## 이번 업로드본 기준 추가 잔존 이슈 요약

1. `settlement-go`가 여전히 `go.work`, README/PROJECT_MAP/AGENTS, Docker build context, command enum, config env에 남아 있습니다.
2. settlement DB migration(`038`, `039`)이 여전히 자동 적용 대상입니다.
3. settlement 제거를 미래에 다시 깨뜨릴 수 있는 **regression gate**가 없습니다.
4. `.worktrees/`, `artifacts/`, `BUNDLE_MANIFEST.txt`가 review bundle / Docker build context를 오염시킬 수 있습니다.
5. `applyScraperProxyToggle(...)`, `infraResources` 같은 thin wrapper / local adapter가 bot과 stream-ingester에 중복되어 있습니다.
6. `shared-go/pkg/logging`과 `hololive-shared/internal/logging`이 공존해 logging SSOT가 이원화되어 있습니다.
7. `MemberDataProvider`의 multi-result contract(`FindMembersByName`, `FindMembersByAlias`)가 실제 adapter에서 빈 slice stub입니다.
8. major event consensus 쪽 deadline budget TODO가 아직 남아 있습니다.
9. admin-dashboard backend는 typed holo contract를 이미 갖고 있는데도 wildcard proxy path가 여전히 살아 있습니다.
10. admin-dashboard frontend는 generated client singleton, 401 처리, trivial route wrapper, OpenAPI drift CI가 아직 정리되지 않았습니다.
11. `shared-go` package allowlist가 stale 상태인데 gate는 이를 fail시키지 않습니다.

---

## 1. Settlement 제거를 “완전한 삭제”로 마감

### 1-1. 워크스페이스와 런타임 인벤토리에서 `settlement-go` 제거

#### 변경 파일
- `go.work`
- `README.md`
- `AGENTS.md`
- `docs/current/PROJECT_MAP.md`
- `docs/PROJECT_MAP.md`는 bridge라 유지 가능. 단, 현재 SSOT 설명만 유지.

#### diff

```diff
diff --git a/go.work b/go.work
@@
 use (
 	./shared-go
 	./hololive/hololive-dispatcher-go
 	./hololive/hololive-kakao-bot-go
 	./hololive/hololive-llm-sched
 	./hololive/hololive-shared
 	./hololive/hololive-stream-ingester
-	./hololive/settlement-go
 )
```

```diff
diff --git a/README.md b/README.md
@@
-| Runtime | Go | bot(+admin API), dispatcher-go, llm-scheduler, settlement, stream-ingester, youtube-scraper |
+| Runtime | Go | bot(+admin API), dispatcher-go, llm-scheduler, stream-ingester, youtube-scraper |
@@
-### Go 모듈 (7개, go.work: 런타임 5 + 라이브러리 2)
+### Go 모듈 (6개, go.work: 런타임 4 + 라이브러리 2)
@@
-| `settlement-go` | Settlement service runtime | 30002 |
 | `hololive-stream-ingester` | Photo sync + ingestion-adjacent runtime builders (`stream-ingester`, `youtube-scraper`) | 30004 / 30005 |
@@
-### Runtime 바이너리 (6개)
+### Runtime 바이너리 (5개)
@@
-| `settlement` | Settlement service runtime | 30002 |
 | `stream-ingester` | Photo sync + ingestion-adjacent health/config runtime | 30004 |
 | `youtube-scraper` | YouTube polling/scraping + outbox runtime | 30005 |
@@
-현재 `docker-compose.prod.yml` 운영 스택은 `bot`, `dispatcher-go`, `llm-scheduler`, `stream-ingester`, `youtube-scraper` 5개 서비스 기준입니다. `settlement`는 워크스페이스/바이너리 인벤토리에는 포함되지만, 현재 compose 배포 스택에는 연결되어 있지 않습니다.
+현재 `docker-compose.prod.yml` 운영 스택은 `bot`, `dispatcher-go`, `llm-scheduler`, `stream-ingester`, `youtube-scraper` 5개 서비스 기준입니다.
@@
-  ./hololive/settlement-go/...
@@
-  ./hololive/settlement-go/...
@@
-`settlement`는 현재 `docker-compose.prod.yml` 서비스가 아니므로 위 compose 재배포 목록에는 포함되지 않습니다.
@@
-- `settlement`는 현재 compose 로그/health 목록 대상이 아닙니다.
```

```diff
diff --git a/AGENTS.md b/AGENTS.md
@@
-It includes the Kakao bot, dispatcher, LLM scheduler, stream ingester, settlement service, shared libraries, and the admin dashboard.
+It includes the Kakao bot, dispatcher, LLM scheduler, stream ingester, shared libraries, and the admin dashboard.
@@
-go build ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-dispatcher-go/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/... ./hololive/settlement-go/...
-go test ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-dispatcher-go/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/... ./hololive/settlement-go/...
+go build ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-dispatcher-go/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/...
+go test ./shared-go/... ./hololive/hololive-shared/... ./hololive/hololive-dispatcher-go/... ./hololive/hololive-kakao-bot-go/... ./hololive/hololive-llm-sched/... ./hololive/hololive-stream-ingester/...
```

```diff
diff --git a/docs/current/PROJECT_MAP.md b/docs/current/PROJECT_MAP.md
@@
-| `settlement-go` | Go 1.26 | `hololive/settlement-go/` | Settlement service runtime | 30002 |
 | `hololive-stream-ingester` | Go 1.26 | `hololive/hololive-stream-ingester/` | Photo sync + ingestion-adjacent runtime | 30004 |
@@
-## Runtime Binaries (6)
+## Runtime Binaries (5)
@@
-| `settlement` | `settlement-go` | 30002 |
 | `stream-ingester` | `hololive-stream-ingester` | 30004 |
 | `youtube-scraper` | `hololive-stream-ingester` | 30005 |
@@
-- Architecture: Go single-language runtime (6 binaries: bot + dispatcher-go + llm-scheduler + settlement + stream-ingester + youtube-scraper).
+- Architecture: Go single-language runtime (5 binaries: bot + dispatcher-go + llm-scheduler + stream-ingester + youtube-scraper).
```

#### 의사결정 이유

지금 상태는 “서비스는 이미 꺼졌는데 인벤토리에는 남아 있는” 전형적인 drift입니다.  
이런 상태는 새 팀원이 README만 보고 존재하지 않는 런타임을 되살리게 만들고, Docker build context도 불필요하게 키웁니다.

---

### 1-2. 실제 코드를 repo에서 제거

#### 변경 파일
- `hololive/settlement-go/` 전체 삭제
- `.gitignore`
- Go 모듈/도구가 `settlement-go`를 보는 모든 Dockerfile

#### 삭제 / 수정

```bash
git rm -r hololive/settlement-go
```

```diff
diff --git a/.gitignore b/.gitignore
@@
-hololive/settlement-go/settlement
+artifacts/
```

#### Dockerfile 정리

다음 5개 Dockerfile에서 `COPY hololive/settlement-go ./hololive/settlement-go` 라인을 제거하십시오.

- `hololive/hololive-dispatcher-go/Dockerfile`
- `hololive/hololive-kakao-bot-go/Dockerfile`
- `hololive/hololive-llm-sched/Dockerfile`
- `hololive/hololive-stream-ingester/Dockerfile`
- `hololive/hololive-stream-ingester/Dockerfile.youtube-scraper`

공통 diff:

```diff
diff --git a/hololive/hololive-kakao-bot-go/Dockerfile b/hololive/hololive-kakao-bot-go/Dockerfile
@@
-COPY hololive/settlement-go ./hololive/settlement-go
```

#### 의사결정 이유

현재 compose 서비스에는 없는데 Docker build context에는 계속 들어갑니다.  
이건 이미지 빌드 캐시 효율만 나쁘게 하고, 제거 대상 런타임이 “사실상 아직 중요 dependency인 것처럼” 보이게 만듭니다.

---

### 1-3. 공용 command enum과 config residue 제거

#### 변경 파일
- `hololive/hololive-shared/pkg/domain/command.go`
- `hololive/hololive-shared/pkg/config/config_types.go`
- `hololive/hololive-shared/pkg/config/config.go`

#### diff

```diff
diff --git a/hololive/hololive-shared/pkg/domain/command.go b/hololive/hololive-shared/pkg/domain/command.go
@@
-	// CommandSettlementStatus: 정산 현황 조회 명령어
-	CommandSettlementStatus CommandType = "settlement_status"
-	// CommandSettlementPaid: 정산 완료 처리 명령어
-	CommandSettlementPaid CommandType = "settlement_paid"
-	// CommandSettlementRegister: 정산 멤버 등록 명령어
-	CommandSettlementRegister CommandType = "settlement_register"
 	// CommandUnknown: 인식할 수 없는 명령어
 	CommandUnknown CommandType = "unknown"
@@
 	case CommandLive, CommandUpcoming, CommandSchedule, CommandHelp,
 		CommandAlarmAdd, CommandAlarmRemove, CommandAlarmList, CommandAlarmClear, CommandAlarmInvalid,
 		CommandMemberInfo, CommandStats, CommandSubscriber,
 		CommandMemberNews, CommandMemberNewsSubscription,
 		CommandMajorEvent,
-		CommandSettlementStatus, CommandSettlementPaid, CommandSettlementRegister,
 		CommandUnknown:
```

```diff
diff --git a/hololive/hololive-shared/pkg/config/config_types.go b/hololive/hololive-shared/pkg/config/config_types.go
@@
 type BotConfig struct {
 	Prefix           string
 	SelfUser         string
 	AdminEnabled     bool
-	SettlementRoomID string // 정산 알람 대상 방 ID (빈 문자열이면 비활성)
 	MentionPrefix    string // 멘션 기반 명령어 접두사 (예: @카푸봇)
 }
```

```diff
diff --git a/hololive/hololive-shared/pkg/config/config.go b/hololive/hololive-shared/pkg/config/config.go
@@
 		Bot: BotConfig{
 			Prefix:           envutil.String("BOT_PREFIX", "!"),
 			SelfUser:         envutil.String("BOT_SELF_USER", "iris"),
 			AdminEnabled:     envutil.Bool("BOT_ADMIN_ENABLED", true),
-			SettlementRoomID: envutil.String("SETTLEMENT_ROOM_ID", ""),
 			MentionPrefix:    envutil.String("BOT_MENTION_PREFIX", "#kapu봇"),
 		},
```

#### 의사결정 이유

`settlement-go`를 지우더라도, shared domain/config에 settlement command와 env가 남아 있으면 “나중에 쓰려고 남겨둔 dormant feature”처럼 보입니다.  
제거 대상이면 공유 계약에서도 같이 빠져야 합니다.

---

### 1-4. bot 테스트의 settlement 잔재 제거

#### 변경 파일
- `hololive/hololive-kakao-bot-go/internal/bot/command_normalizer_test.go`
- `hololive/hololive-kakao-bot-go/internal/bot/bot_command_init_views_test.go`

#### diff

`command_normalizer_test.go`에서 아래 케이스 삭제:

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/bot/command_normalizer_test.go b/hololive/hololive-kakao-bot-go/internal/bot/command_normalizer_test.go
@@
-		{
-			name:    "settlement_status → 변환 없이 원래 타입 유지",
-			cmdType: domain.CommandSettlementStatus,
-			wantKey: "settlement_status",
-		},
```

`bot_command_init_views_test.go`에서 settlement status init command test 블록 삭제:

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/bot/bot_command_init_views_test.go b/hololive/hololive-kakao-bot-go/internal/bot/bot_command_init_views_test.go
@@
-	target := &stubCommandInitCommand{name: string(domain.CommandSettlementStatus)}
-...
-			_, err := deps.Dispatcher.Publish(ctx, cmdCtx, command.Event{Type: domain.CommandSettlementStatus})
```

#### 의사결정 이유

이 테스트들은 실제 기능을 보호하지 않습니다. 오히려 제거된 feature를 계약처럼 남깁니다.

---

### 1-5. settlement migration을 “자동 적용”에서 빼고, 수동 퇴역 절차로 이동

#### 문제

현재 `hololive/hololive-kakao-bot-go/scripts/migrations/apply-all.sh`는 `[0-9]*.sql`을 전부 적용합니다.  
따라서 `038_create_settlement.sql`과 `039_create_settlement_v2.sql`이 계속 fresh DB에 반영됩니다.

#### 변경 파일
- `hololive/hololive-kakao-bot-go/scripts/migrations/038_create_settlement.sql`
- `hololive/hololive-kakao-bot-go/scripts/migrations/039_create_settlement_v2.sql`
- 새 파일: `hololive/hololive-kakao-bot-go/scripts/migrations/archive/settlement/038_create_settlement.sql`
- 새 파일: `hololive/hololive-kakao-bot-go/scripts/migrations/archive/settlement/039_create_settlement_v2.sql`
- 새 파일: `hololive/hololive-kakao-bot-go/scripts/migrations/manual/settlement_drop.sql`
- 새 문서: `docs/runbook_execution/SETTLEMENT_DECOMMISSION_RUNBOOK.md`

#### 이동

```bash
mkdir -p hololive/hololive-kakao-bot-go/scripts/migrations/archive/settlement
git mv hololive/hololive-kakao-bot-go/scripts/migrations/038_create_settlement.sql hololive/hololive-kakao-bot-go/scripts/migrations/archive/settlement/038_create_settlement.sql
git mv hololive/hololive-kakao-bot-go/scripts/migrations/039_create_settlement_v2.sql hololive/hololive-kakao-bot-go/scripts/migrations/archive/settlement/039_create_settlement_v2.sql
```

#### 새 수동 drop 스크립트

```sql
-- hololive/hololive-kakao-bot-go/scripts/migrations/manual/settlement_drop.sql
BEGIN;

DROP TABLE IF EXISTS settlement_payment_events_v2 CASCADE;
DROP TABLE IF EXISTS settlement_payments_v2 CASCADE;
DROP TABLE IF EXISTS settlement_cycles_v2 CASCADE;
DROP TABLE IF EXISTS settlement_member_terms CASCADE;
DROP TABLE IF EXISTS settlement_room_configs CASCADE;

DROP TABLE IF EXISTS settlement_payments CASCADE;
DROP TABLE IF EXISTS settlement_cycles CASCADE;
DROP TABLE IF EXISTS settlement_members CASCADE;

COMMIT;
```

#### 새 runbook 초안

```md
# Settlement Decommission Runbook

## 목적
운영 DB에서 settlement 잔재 테이블을 안전하게 제거한다.

## 원칙
- 이 절차는 수동 실행이다.
- 일반 bootstrap / deploy 파이프라인에 포함하지 않는다.
- 먼저 export/backup 후 drop 한다.

## 절차
1. 대상 DB에서 settlement 관련 테이블 row count 확인
2. 필요 시 CSV 또는 pg_dump table-level export 수행
3. maintenance window 확보
4. `scripts/migrations/manual/settlement_drop.sql` 수동 실행
5. drop 후 schema 검증
6. release note와 운영 로그에 decommission 완료 기록

## 검증 SQL
SELECT to_regclass('public.settlement_members');
SELECT to_regclass('public.settlement_cycles');
SELECT to_regclass('public.settlement_payments');
SELECT to_regclass('public.settlement_room_configs');
SELECT to_regclass('public.settlement_member_terms');
SELECT to_regclass('public.settlement_cycles_v2');
SELECT to_regclass('public.settlement_payments_v2');
SELECT to_regclass('public.settlement_payment_events_v2');
```

#### 의사결정 이유

정산을 제거한다는 이유로 일반 deploy 시점에 자동 `DROP TABLE`을 넣으면 운영 리스크가 너무 큽니다.  
올바른 방식은 “fresh 환경에는 더 이상 생기지 않게 하고, 기존 환경 drop은 수동 runbook으로 분리”입니다.

---

### 1-6. settlement historical docs는 archive로 이동, active docs에서는 제거

#### 이동 대상
- `docs/superpowers/specs/2026-03-17-settlement-bot-design.md`
- `docs/superpowers/specs/2026-03-18-settlement-go-separation-design.md`
- `docs/superpowers/plans/2026-03-18-settlement-go-separation.md`
- `docs/superpowers/plans/2026-03-18-settlement-v2-anchor-cycle.md`

#### 권장 이동

```bash
mkdir -p docs/history/settlement
git mv docs/superpowers/specs/2026-03-17-settlement-bot-design.md docs/history/settlement/
git mv docs/superpowers/specs/2026-03-18-settlement-go-separation-design.md docs/history/settlement/
git mv docs/superpowers/plans/2026-03-18-settlement-go-separation.md docs/history/settlement/
git mv docs/superpowers/plans/2026-03-18-settlement-v2-anchor-cycle.md docs/history/settlement/
```

#### 수정 대상
- `docs/superpowers/specs/2026-03-23-iris-standardization-excluding-game-bot-design.md`
- `docs/superpowers/plans/2026-03-23-iris-standardization-excluding-game-bot.md`

두 문서는 settlement-only 문서가 아니라 Iris 표준화 문서이므로 삭제가 아니라 **settlement scope 제거**가 맞습니다.

##### design 문서 diff

```diff
diff --git a/docs/superpowers/specs/2026-03-23-iris-standardization-excluding-game-bot-design.md b/docs/superpowers/specs/2026-03-23-iris-standardization-excluding-game-bot-design.md
@@
 - `/home/kapu/gemini/hololive-bot`
 - `/home/kapu/gemini/chat-bot-go-kakao`
-- `/home/kapu/gemini/settlement-go`
 - `/home/kapu/gemini/iris-client-go`
 - `/home/kapu/gemini/Iris`
@@
-2. `settlement-go`는 `iris-client-go` client는 쓰지만 서버측 webhook wiring은 직접 구현합니다.
 3. `Iris` 운영 문서가 실제 webhook 소비자 목록을 완전히 반영하지 않습니다.
@@
-6. `settlement-go`는 현재 outbound `IRIS_BOT_TOKEN`만 배선되어 있고 inbound `IRIS_WEBHOOK_TOKEN`은 배포/env 예제에 없습니다.
-7. `hololive-bot`는 legacy ...
+6. `hololive-bot`는 legacy ...
@@
-- `settlement-go`의 webhook/client wiring 표준화
 - `chat-bot-go-kakao`와 `hololive-bot`의 Iris 초기화 패턴을 preset 기준으로 정리
@@
-2. `settlement-go`는 direct webhook plumbing 대신 SDK 기반 wiring을 사용해야 합니다.
-3. `chat-bot-go-kakao`와 `hololive-bot`는 ...
+2. `chat-bot-go-kakao`와 `hololive-bot`는 ...
@@
-- `settlement-go`의 webhook 경로/토큰/응답/thread-id 관련 회귀 테스트가 통과한다.
 - `chat-bot-go-kakao`와 `hololive-bot`의 Iris factory 관련 테스트가 통과한다.
@@
-- `settlement-go/.env.example`, `settlement-go/docker-compose.prod.yml`, 필요한 consumer docs가 ...
-- `/home/kapu/gemini/go.work` 기준에서 `iris-client-go` 변경이 `settlement-go`와 `chat-bot-go-kakao`에서 ...
+- `/home/kapu/gemini/go.work` 기준에서 `iris-client-go` 변경이 `chat-bot-go-kakao`와 `hololive-bot`에서 ...
```

##### plan 문서 diff

`Task 2: Migrate settlement-go ...` 전체 섹션을 삭제하고, Task 번호를 재정렬하십시오.  
또한 Architecture paragraph와 verification bullets에서 `settlement-go`를 제거하십시오.

#### 의사결정 이유

active planning/spec 문서에 제거 대상 런타임이 계속 남아 있으면, 이후 작업자가 “이 계획이 아직 살아 있다”고 오해합니다.

---

### 1-7. settlement 재유입 방지용 architecture gate 추가

#### 새 파일
- `scripts/architecture/check-removed-runtime-references.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

matches="$(
  rg -n \
    "(settlement-go|CommandSettlementStatus|CommandSettlementPaid|CommandSettlementRegister|settlement_status|settlement_paid|settlement_register|SETTLEMENT_ROOM_ID)" \
    "${ROOT_DIR}" \
    --glob '!docs/history/settlement/**' \
    --glob '!hololive/hololive-kakao-bot-go/scripts/migrations/archive/settlement/**' \
    --glob '!hololive/hololive-kakao-bot-go/scripts/migrations/manual/settlement_drop.sql' \
    --glob '!.worktrees/**' \
    --glob '!**/*.tar.gz' \
    --glob '!**/node_modules/**' \
    || true
)"

if [[ -n "${matches}" ]]; then
  echo "FAIL: removed settlement runtime references detected" >&2
  echo "${matches}" >&2
  exit 1
fi

echo "OK: no removed settlement runtime references detected"
```

#### `ci-boundary-gate.sh` 연결

```diff
diff --git a/scripts/architecture/ci-boundary-gate.sh b/scripts/architecture/ci-boundary-gate.sh
@@
 echo "[M0] go compatibility adapter check"
 "${SCRIPT_DIR}/check-go-compat-adapters.sh"
 echo
+
+echo "[M0] removed runtime reference check"
+"${SCRIPT_DIR}/check-removed-runtime-references.sh"
+echo
```

#### 의사결정 이유

지금 문제는 “한 번 지웠다가 다시 슬쩍 들어오는” 회귀입니다.  
removed runtime에 대해서는 boundary gate가 있어야 합니다.

---

## 2. Review bundle / Docker build context 오염 제거

### 문제

업로드 번들에 `.worktrees/`가 실제로 포함되어 있었습니다.  
현재 `.dockerignore`는 `.worktrees/`와 `artifacts/`, `BUNDLE_MANIFEST.txt`를 막지 않습니다.

### 변경 파일
- `.dockerignore`
- `.gitignore`
- 새 파일: `scripts/review/export-source-bundle.sh`
- 새 문서: `docs/current/review-bundles.md`

### diff

```diff
diff --git a/.dockerignore b/.dockerignore
@@
 .tasklists/
 .runlogs/
+.worktrees/
+.deploy-snapshots/
+artifacts/
+BUNDLE_MANIFEST.txt
@@
 **/coverage
+**/artifacts
```

```diff
diff --git a/.gitignore b/.gitignore
@@
 backups/
+artifacts/
```

### 새 번들 export 스크립트

```bash
#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_DIR="${1:-${ROOT_DIR}/artifacts/review}"
OUT_FILE="${OUT_DIR}/hololive-bot-source-${STAMP}.tar.gz"

mkdir -p "${OUT_DIR}"

tar \
  --exclude-vcs \
  --exclude='.worktrees' \
  --exclude='.tasklists' \
  --exclude='.runlogs' \
  --exclude='.codex' \
  --exclude='.claude' \
  --exclude='.serena' \
  --exclude='.gemini' \
  --exclude='artifacts' \
  --exclude='logs' \
  --exclude='**/node_modules' \
  --exclude='**/dist' \
  --exclude='**/coverage' \
  --exclude='*.tar.gz' \
  --exclude='BUNDLE_MANIFEST.txt' \
  -czf "${OUT_FILE}" \
  -C "${ROOT_DIR}" .

echo "${OUT_FILE}"
```

### 의사결정 이유

지금 상태는 코드 리뷰 번들에 hidden worktree까지 섞여 들어갈 수 있습니다.  
이건 단순 미관 문제가 아니라, 리뷰 범위와 build context가 실제 repo root와 달라지는 문제입니다.

---

## 3. Thin wrapper / local adapter 과증식 제거

### 3-1. `applyScraperProxyToggle(...)` 중복 wrapper 삭제

#### 변경 파일
- 삭제: `hololive/hololive-kakao-bot-go/internal/app/bootstrap_bot_proxy_toggle.go`
- 삭제: `hololive/hololive-stream-ingester/internal/app/runtime_helpers.go`

현재 두 파일은 사실상 아래 한 줄 wrapper입니다.

```go
sharedsettings.ApplyScraperProxyToggle(enabled, youtubeService, holodexService, scraperScheduler, logger)
```

#### call-site 교체

기존:

```go
applyScraperProxyToggle(payload.Enabled, ytStack.GetService(), holodexService, scraperScheduler, logger)
```

교체:

```go
sharedsettings.ApplyScraperProxyToggle(payload.Enabled, ytStack.GetService(), holodexService, scraperScheduler, logger)
```

#### 의사결정 이유

이 함수들은 추상화가 아니라 ceremony입니다.  
이런 wrapper가 누적되면 “shared function이 있는데도 runtime마다 local helper가 하나씩 생기는” AI 냄새가 납니다.

---

### 3-2. `infraResources` duplication 제거

#### 문제

bot과 stream-ingester가 모두 `sharedmodules.BuildInfraModule(...)` 결과를 거의 동일한 local struct로 감싸고 있습니다.  
게다가 `cleanupDB: func(){}` 같은 더미 필드까지 들어 있습니다.

#### 권장 방향

local wrapper를 없애고 `sharedmodules.InfraModule`을 직접 쓰거나, 정말 필요한 경우 얇은 alias만 둡니다.

#### 변경 파일
- `hololive/hololive-kakao-bot-go/internal/app/bootstrap_core.go`
- `hololive/hololive-stream-ingester/internal/app/bootstrap.go`

#### diff 예시 (bot)

```diff
diff --git a/hololive/hololive-kakao-bot-go/internal/app/bootstrap_core.go b/hololive/hololive-kakao-bot-go/internal/app/bootstrap_core.go
@@
-import (
-    ...
-    "github.com/kapu/hololive-shared/pkg/service/cache"
-    "github.com/kapu/hololive-shared/pkg/service/database"
-    "github.com/kapu/hololive-shared/pkg/service/member"
-)
+import (
+    ...
+)
@@
-type infraResources struct {
-	cacheService    cache.Client
-	postgresService database.Client
-	memberRepo      *member.Repository
-	memberCache     *member.Cache
-	cleanupCache    func()
-	cleanupDB       func()
-}
+type infraResources = sharedmodules.InfraModule
@@
-func initInfraResources(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*infraResources, error) {
-	module, err := sharedmodules.BuildInfraModule(ctx, cfg, logger)
-	if err != nil {
-		return nil, fmt.Errorf("provide infra resources: %w", err)
-	}
-
-	return &infraResources{
-		cacheService:    module.Cache,
-		postgresService: module.Postgres,
-		memberRepo:      module.MemberRepo,
-		memberCache:     module.MemberCache,
-		cleanupCache:    module.Cleanup,
-		cleanupDB:       func() {},
-	}, nil
+func initInfraResources(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*infraResources, error) {
+	module, err := sharedmodules.BuildInfraModule(ctx, cfg, logger)
+	if err != nil {
+		return nil, fmt.Errorf("provide infra resources: %w", err)
+	}
+	return module, nil
 }
```

이후 call-site는 `infra.Cache`, `infra.Postgres`, `infra.MemberRepo`, `infra.MemberCache`, `infra.Cleanup` 기준으로 바꾸십시오.

#### 의사결정 이유

지금 local struct는 정보 추가가 아니라 field renaming과 dummy cleanup 분리만 합니다.  
이건 실제 로직을 숨기고 인지부하만 높입니다.

---

### 3-3. Runtime router helper 중복 정리

현재 아래 함수들이 사실상 모두 `sharedserver.NewRuntimeRouter(...)` wrapper입니다.

- `hololive-kakao-bot-go/internal/app/ProvideHealthOnlyRouter`
- `hololive-kakao-bot-go/internal/app/ProvideTriggerRouter`
- `hololive-llm-sched/internal/app/ProvideHealthOnlyRouter`
- `hololive-llm-sched/internal/app/ProvideTriggerRouter`
- `hololive-stream-ingester/internal/app/ProvideHealthOnlyRouter`

#### 개선안

새 파일:

- `hololive/hololive-shared/pkg/server/runtime_router_factories.go`

```go
package server

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"
)

func NewHealthOnlyRuntimeRouter(ctx context.Context, logger *slog.Logger, apiKey string, opts ...func(*RuntimeRouterOptions)) (*gin.Engine, error) {
	options := RuntimeRouterOptions{APIKey: apiKey}
	for _, opt := range opts {
		opt(&options)
	}
	return NewRuntimeRouter(ctx, logger, options)
}

func NewTriggerRuntimeRouter(ctx context.Context, logger *slog.Logger, triggerHandler *TriggerHandler, apiKey string, opts ...func(*RuntimeRouterOptions)) (*gin.Engine, error) {
	options := RuntimeRouterOptions{
		APIKey: apiKey,
		RegisterRoutes: func(router *gin.Engine) error {
			if triggerHandler == nil {
				return nil
			}
			if strings.TrimSpace(apiKey) == "" {
				return errors.New("API_SECRET_KEY required")
			}
			triggerHandler.RegisterInternalRoutesWithAuth(router.Group(""), apiKey)
			return nil
		},
	}
	for _, opt := range opts {
		opt(&options)
	}
	return NewRuntimeRouter(ctx, logger, options)
}
```

각 런타임은 이제 local wrapper를 들고 있지 말고 directly shared helper를 호출하십시오.  
stream-ingester의 readiness responder만 option으로 주입하면 됩니다.

#### 의사결정 이유

이 부분은 기능 버그보다 AI 냄새 제거 목적이 큽니다.  
현재 구조는 동일 개념의 router builder가 런타임마다 재정의되어 있습니다.

---

## 4. Logging SSOT를 `shared-go/pkg/logging` 하나로 통일

### 문제

지금 logging 구현이 세 겹입니다.

1. `shared-go/pkg/logging/*`
2. `hololive/hololive-shared/internal/logging/*`
3. `hololive/hololive-shared/pkg/logging/*` (2를 감싸는 wrapper)

그런데 운영 문서가 기대하는 `logs/archive/*.gz` archive behavior는 `hololive-shared/internal/logging` 쪽에 있고, shared-go 쪽은 OTel correlation이 더 풍부합니다.  
즉 두 구현이 각자 장점을 반반 들고 있습니다.

### 최종 목표

- **실제 구현 SSOT는 `shared-go/pkg/logging`**
- `hololive-shared/pkg/logging`은 shared-go로 연결되는 thin facade
- `hololive-shared/internal/logging`은 삭제

### 적용 순서

#### 4-1. internal 구현을 shared-go로 흡수

가장 현실적인 방법은 “internal logging의 archive-aware implementation”을 `shared-go/pkg/logging`으로 옮기고, 거기에 현재 shared-go의 OTel handler 기능을 합치는 것입니다.

#### 변경 방식

1. `hololive/hololive-shared/internal/logging/logging.go`의 archive 관련 구현을 `shared-go/pkg/logging/logging.go`로 이동
2. `shared-go/pkg/logging/logging.go`의 기존 `EnableFileLoggingWithOTel`, `OTelHandler`, `NewOTelHandler`는 유지
3. `EnableFileLogging`은 `EnableFileLoggingWithOTel(cfg, fileName, false)` 호출로 통일
4. `EnableFileLoggingWithOTel` 내부에서 archive-aware writer를 사용하도록 교체

#### 핵심 diff 개념

```diff
diff --git a/shared-go/pkg/logging/logging.go b/shared-go/pkg/logging/logging.go
@@
-import (
-	"context"
-	"errors"
-	"fmt"
-	"io"
-	"log/slog"
-	"os"
-	"path/filepath"
-	"strings"
-	"time"
+import (
+	"context"
+	"errors"
+	"fmt"
+	"io"
+	"log/slog"
+	"os"
+	"path/filepath"
+	"slices"
+	"strings"
+	"sync"
+	"time"
@@
+	"github.com/mattn/go-isatty"
@@
+	compressSuffix      = ".gz"
+	backupTimeFormat    = "2006-01-02T15-04-05.000"
+	archiveDirName      = "archive"
+	archiveScanInterval = 5 * time.Second
```

그리고 `EnableFileLoggingWithOTel` 내부는 기존 shared-go의 `combined.log` 방식 대신, internal logging의 `archiveAwareWriter` + `compressedLogArchiver` 로직을 사용하도록 바꾸십시오.

#### 4-2. `hololive-shared/pkg/logging`을 shared-go facade로 교체

```diff
diff --git a/hololive/hololive-shared/pkg/logging/logging.go b/hololive/hololive-shared/pkg/logging/logging.go
@@
-import (
-	"fmt"
-	"io"
-	"log/slog"
-
-	internallogging "github.com/kapu/hololive-shared/internal/logging"
-)
+import (
+	"fmt"
+	"io"
+	"log/slog"
+
+	sharedlogging "github.com/park285/llm-kakao-bots/shared-go/pkg/logging"
+)
@@
-type Config = internallogging.Config
+type Config = sharedlogging.Config
@@
-	return internallogging.NewLogger()
+	return sharedlogging.NewLogger()
@@
-	logger, err := internallogging.EnableFileLogging(cfg, "")
+	logger, err := sharedlogging.EnableFileLogging(cfg, "")
@@
-	return internallogging.NewTestLogger()
+	return sharedlogging.NewTestLogger()
@@
-	return internallogging.NewTestLoggerWithOutput(w)
+	return sharedlogging.NewTestLoggerWithOutput(w)
@@
-	logger, err := internallogging.EnableFileLogging(cfg, fileName)
+	logger, err := sharedlogging.EnableFileLogging(cfg, fileName)
```

#### 4-3. internal logging imports 정리 후 삭제

검색 결과 direct internal import가 남아 있는 파일:
- `hololive/hololive-shared/internal/testutil/cache.go`
- `hololive/hololive-shared/pkg/service/auth/service_test.go`
- `hololive/hololive-shared/pkg/service/delivery/locker_test.go`
- `hololive/hololive-shared/pkg/service/alarm/queue/queue_test.go`

이들은 전부 `github.com/kapu/hololive-shared/pkg/logging` 또는 `shared-go/pkg/logging`으로 바꾸십시오.

마지막에:

```bash
git rm -r hololive/hololive-shared/internal/logging
```

#### 의사결정 이유

shared utility가 이미 있는데, richer behavior는 internal에만 있고 facade는 다른 패키지를 감싸고 있는 구조는 가장 나쁜 이원화입니다.  
logging은 인프라 성격이 강하므로 반드시 SSOT가 하나여야 합니다.

---

## 5. Member adapter의 multi-result contract를 실제 구현으로 채우기

### 문제

`hololive/hololive-shared/pkg/service/member/adapter.go`의 아래 두 함수가 현재 stub입니다.

```go
func (a *ServiceAdapter) FindMembersByName(name string) []*domain.Member {
    return []*domain.Member{}
}

func (a *ServiceAdapter) FindMembersByAlias(alias string) []*domain.Member {
    return []*domain.Member{}
}
```

하지만 `domain.MemberDataProvider` 인터페이스는 multi-result semantics를 계약으로 선언하고 있습니다.

### 변경 파일
- `hololive/hololive-shared/pkg/service/member/adapter.go`
- `hololive/hololive-shared/pkg/service/member/adapter_test.go`

### 권장 구현

```diff
diff --git a/hololive/hololive-shared/pkg/service/member/adapter.go b/hololive/hololive-shared/pkg/service/member/adapter.go
@@
 import (
 	"context"
 	"log/slog"
+	"strings"
@@
 func (a *ServiceAdapter) FindMembersByName(name string) []*domain.Member {
-	return []*domain.Member{}
+	needle := strings.TrimSpace(name)
+	if needle == "" {
+		return []*domain.Member{}
+	}
+
+	members := a.GetAllMembers()
+	matched := make([]*domain.Member, 0, len(members))
+	for _, member := range members {
+		if member == nil {
+			continue
+		}
+		if equalFoldAny(needle, member.Name, member.NameJa, member.NameKo) {
+			matched = append(matched, member)
+		}
+	}
+	return cloneMemberSlice(matched)
 }
@@
 func (a *ServiceAdapter) FindMembersByAlias(alias string) []*domain.Member {
-	return []*domain.Member{}
+	needle := strings.TrimSpace(alias)
+	if needle == "" {
+		return []*domain.Member{}
+	}
+
+	members := a.GetAllMembers()
+	matched := make([]*domain.Member, 0, len(members))
+	for _, member := range members {
+		if member == nil {
+			continue
+		}
+		for _, candidate := range member.GetAllAliases() {
+			if strings.EqualFold(strings.TrimSpace(candidate), needle) {
+				matched = append(matched, member)
+				break
+			}
+		}
+	}
+	return cloneMemberSlice(matched)
 }
+
+func equalFoldAny(target string, values ...string) bool {
+	for _, value := range values {
+		if strings.EqualFold(strings.TrimSpace(value), target) {
+			return true
+		}
+	}
+	return false
+}
+
+func cloneMemberSlice(in []*domain.Member) []*domain.Member {
+	if len(in) == 0 {
+		return []*domain.Member{}
+	}
+	out := make([]*domain.Member, len(in))
+	copy(out, in)
+	return out
+}
```

### 테스트 추가

`adapter_test.go`에 아래 2개를 추가하십시오.

```go
func TestServiceAdapter_FindMembersByName_MatchesLocalizedNames(t *testing.T) {
	// repo stub + cache 구성
	// Name, NameJa, NameKo 중 하나로 두 명이 매칭되는 케이스 검증
}

func TestServiceAdapter_FindMembersByAlias_ReturnsAllAliasMatches(t *testing.T) {
	// 동일 alias를 공유하는 두 멤버가 모두 반환되는지 검증
}
```

#### 의사결정 이유

지금은 인터페이스가 약속한 기능과 실제 adapter가 다릅니다.  
이건 “사용 안 하니 비워 둔” 상태가 아니라 **조용한 기능 결손**입니다.

---

## 6. major event consensus에 parent deadline budget 반영

### 문제

`hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer_consensus.go`에 아직도 `// consensus: deadline budget TODO`가 남아 있습니다.

현재는 `review`와 `adjudication` 모두 parent context의 남은 시간을 고려하지 않고 고정 timeout을 씁니다.  
이 경우 상위 deadline이 이미 촉박해도 하위 단계가 의미 없이 timeout을 더 길게 잡습니다.

### 변경 파일
- `hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer_consensus.go`
- 새 테스트: `summarizer_consensus_test.go` 또는 existing consensus test file

### 권장 구현

```diff
diff --git a/hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer_consensus.go b/hololive/hololive-llm-sched/internal/service/majorevent/summarizer/summarizer_consensus.go
@@
 import (
 	"context"
 	"fmt"
 	"log/slog"
 	"strings"
+	"time"
@@
-	// consensus: deadline budget TODO
 	if primary == nil || s.reviewer == nil {
 		return primary, false
 	}
 
-	reviewCtx, cancel := context.WithTimeout(ctx, s.consensus.ReviewTimeout)
+	reviewCtx, cancel, ok := deriveConsensusBudget(ctx, s.consensus.ReviewTimeout, 250*time.Millisecond)
+	if !ok {
+		s.logger.Warn("major event consensus skipped: insufficient budget for review")
+		return primary, false
+	}
 	defer cancel()
@@
-	adjCtx, adjCancel := context.WithTimeout(ctx, s.consensus.AdjudicateTimeout)
+	adjCtx, adjCancel, ok := deriveConsensusBudget(ctx, s.consensus.AdjudicateTimeout, 250*time.Millisecond)
+	if !ok {
+		s.logger.Warn("major event consensus skipped: insufficient budget for adjudication")
+		return primary, false
+	}
 	defer adjCancel()
@@
 }
+
+func deriveConsensusBudget(parent context.Context, requested, reserve time.Duration) (context.Context, context.CancelFunc, bool) {
+	if requested <= 0 {
+		requested = time.Second
+	}
+	if reserve < 0 {
+		reserve = 0
+	}
+
+	if deadline, ok := parent.Deadline(); ok {
+		remaining := time.Until(deadline) - reserve
+		if remaining <= 0 {
+			return nil, nil, false
+		}
+		if remaining < requested {
+			requested = remaining
+		}
+	}
+
+	ctx, cancel := context.WithTimeout(parent, requested)
+	return ctx, cancel, true
+}
```

### 테스트

```go
func TestDeriveConsensusBudget_CapsToParentDeadline(t *testing.T) {}
func TestDeriveConsensusBudget_ReturnsFalseWhenNoBudgetLeft(t *testing.T) {}
```

#### 의사결정 이유

이건 기능 추가가 아니라 failure mode 정리입니다.  
deadline을 무시하는 consensus는 전체 scheduler SLA를 예측 불가능하게 만듭니다.

---

## 7. Admin Dashboard backend에서 wildcard holo proxy 제거

### 문제

backend는 이미 `crate::holo::handlers::*`와 `HoloApiClient`를 통해 typed contract를 갖고 있습니다.  
그런데 동시에 `/admin/api/holo/{*path}` wildcard proxy도 살아 있습니다.

이 구조는 두 가지를 망칩니다.

1. backend가 자기 contract를 소유하지 못합니다.
2. 테스트가 typed contract와 proxy path를 동시에 보호하느라 경계가 흐려집니다.

### 변경 파일
- 삭제: `admin-dashboard/backend/src/proxy/mod.rs`
- 삭제: `admin-dashboard/backend/src/proxy/bot_proxy.rs`
- `admin-dashboard/backend/src/main.rs`
- `admin-dashboard/backend/src/state.rs`
- `admin-dashboard/backend/src/routes.rs`
- `admin-dashboard/backend/tests/integration_test.rs`
- `admin-dashboard/backend/src/lib.rs` 또는 module export file에 `pub mod proxy;`가 있으면 같이 제거

### diff

#### `state.rs`

```diff
diff --git a/admin-dashboard/backend/src/state.rs b/admin-dashboard/backend/src/state.rs
@@
-use crate::proxy::BotProxy;
 use crate::status::{StatusCollector, SystemStats};
@@
-    pub bot_proxy: Option<BotProxy>,
     pub holo_api: Arc<HoloApiClient>,
```

#### `main.rs`

```diff
diff --git a/admin-dashboard/backend/src/main.rs b/admin-dashboard/backend/src/main.rs
@@
-mod proxy;
@@
-    let bot_proxy = Some(proxy::BotProxy::new(&cfg.holo_bot_url, {
-        let key = cfg.holo_bot_api_key.clone();
-        if key.is_empty() { None } else { Some(key) }
-    }));
     let holo_api = Arc::new(
         holo::client::HoloApiClient::new(
             cfg.holo_bot_url.clone(),
@@
         config: cfg.clone(),
         sessions,
         rate_limiter: rate_limiter.clone(),
-        bot_proxy,
         holo_api,
         docker_svc,
         status_collector,
```

#### `routes.rs`

```diff
diff --git a/admin-dashboard/backend/src/routes.rs b/admin-dashboard/backend/src/routes.rs
@@
-use axum::{
-    Json, Router, middleware,
-    routing::{any, get, post},
-};
+use axum::{
+    Json, Router, middleware,
+    routing::{get, post},
+};
@@
-    let proxy_routes = Router::new().route(
-        crate::proxy::HOLO_PROXY_ROUTE,
-        any(crate::proxy::bot_proxy::proxy_holo),
-    );
-
     let authenticated = Router::new()
         .merge(auth_csrf)
         .merge(auth_get)
         .merge(holo_routes)
-        .merge(proxy_routes)
         .layer(auth_layer);
```

#### integration test 정리

proxy-specific helper와 테스트를 지우고, typed holo route가 auth layer 뒤에서 401/200을 잘 내는지 확인하는 테스트만 남기십시오.

삭제 대상:
- `use admin_dashboard::proxy::{BotProxy, HOLO_PROXY_ROUTE};`
- `build_proxy_test_app(...)`
- `test_holo_proxy_route_rewrites_and_forwards_successfully`
- proxy header forwarding tests

유지/추가:
- `test_holo_route_without_cookie_returns_401`
- typed `GET /admin/api/holo/members` mocked upstream smoke test
- `test_openapi_includes_holo_paths`는 이미 backend 쪽에 있음

#### 의사결정 이유

typed backend contract가 이미 있는데 wildcard proxy를 병행하는 것은 “이전 구조를 아직 못 버린 상태”입니다.  
이건 admin-dashboard modernization의 마지막 남은 큰 잔재입니다.

---

## 8. Admin Dashboard frontend transport/route 정리

### 8-1. generated Admin client singleton 하나로 통일

#### 문제

현재 둘 다 singleton을 만들고 있습니다.

- `admin-dashboard/frontend/src/api/core.ts`
- `admin-dashboard/frontend/src/api/holoClient.ts`

#### 새 파일
- `admin-dashboard/frontend/src/api/adminClient.ts`

```ts
import { Admin } from '@/api/generated/Admin'
import apiClient from '@/api/client'

export const adminClient = new Admin()
adminClient.instance = apiClient
```

#### diff

```diff
diff --git a/admin-dashboard/frontend/src/api/core.ts b/admin-dashboard/frontend/src/api/core.ts
@@
-import apiClient, { createApiClient } from './client'
-import { Admin } from '@/api/generated/Admin'
+import apiClient from './client'
+import { adminClient } from '@/api/adminClient'
@@
-const adminClient = new Admin()
-adminClient.instance = createApiClient('')
```

```diff
diff --git a/admin-dashboard/frontend/src/api/holoClient.ts b/admin-dashboard/frontend/src/api/holoClient.ts
@@
-import { Admin } from '@/api/generated/Admin'
+import { adminClient } from '@/api/adminClient'
@@
-import { createApiClient } from '@/api/client'
-
-const adminClient = new Admin()
-adminClient.instance = createApiClient('')
```

#### 의사결정 이유

API client singleton이 둘이면 interceptor, baseURL, auth behavior가 미묘하게 갈라질 여지가 생깁니다.

---

### 8-2. stale 401 exemption 제거

#### 문제

`client.ts`는 401 처리에서 `!url.startsWith('/holo/')`일 때만 logout 합니다.  
하지만 generated admin holo path는 `/admin/api/holo/...`입니다. 즉 지금 special case는 stale condition입니다.

#### diff

```diff
diff --git a/admin-dashboard/frontend/src/api/client.ts b/admin-dashboard/frontend/src/api/client.ts
@@
 		if (error.response?.status === 401) {
-			const url = error.config?.url ?? ''
-			if (!url.startsWith('/holo/')) {
-				useAuthStore.getState().logout()
-				queryClient.clear()
-				if (window.location.pathname !== '/login') {
-					window.location.href = '/login'
-				}
-			}
+			useAuthStore.getState().logout()
+			queryClient.clear()
+			if (window.location.pathname !== '/login') {
+				window.location.href = '/login'
+			}
 		}
```

#### 의사결정 이유

현재 special case는 사실상 dead branch입니다.  
이런 stale exception은 나중에 인증 버그를 숨깁니다.

---

### 8-3. trivial `*Tab.tsx` wrappers 삭제

#### 삭제 대상
- `admin-dashboard/frontend/src/components/AlarmsTab.tsx`
- `MembersTab.tsx`
- `MilestonesTab.tsx`
- `RoomsTab.tsx`
- `SettingsTab.tsx`
- `StatsTab.tsx`
- `StreamsTab.tsx`

#### route-definitions 직접 연결

```diff
diff --git a/admin-dashboard/frontend/src/routes/route-definitions.ts b/admin-dashboard/frontend/src/routes/route-definitions.ts
@@
-    load: () => import('@/components/StatsTab'),
+    load: () => import('@/features/stats/pages/StatsPage'),
@@
-    load: () => import('@/components/StreamsTab'),
+    load: () => import('@/features/streams/pages/StreamsPage'),
@@
-    load: () => import('@/components/MembersTab'),
+    load: () => import('@/features/members/pages/MembersPage'),
@@
-    load: () => import('@/components/MilestonesTab'),
+    load: () => import('@/features/milestones/pages/MilestonesPage'),
@@
-    load: () => import('@/components/AlarmsTab'),
+    load: () => import('@/features/alarms/pages/AlarmsPage'),
@@
-    load: () => import('@/components/RoomsTab'),
+    load: () => import('@/features/rooms/pages/RoomsPage'),
@@
-    load: () => import('@/components/SettingsTab'),
+    load: () => import('@/features/settings/pages/SettingsPage'),
```

#### 의사결정 이유

이 파일들은 re-export 한 줄뿐이라 구조 설명력이 없습니다.  
feature-sliced 구조가 이미 도입됐으므로 이제 wrapper는 걷어내는 것이 맞습니다.

---

### 8-4. generated client drift를 CI에서 강제

#### 변경 파일
- `.github/workflows/admin-dashboard-frontend.yml`

#### diff

```diff
diff --git a/.github/workflows/admin-dashboard-frontend.yml b/.github/workflows/admin-dashboard-frontend.yml
@@
       - run: npm ci
       - run: npm run generate:api
+      - name: Verify generated API artifacts are committed
+        run: |
+          git diff --exit-code -- \
+            admin-dashboard/backend/docs/swagger.json \
+            admin-dashboard/frontend/src/api/generated
       - run: npm run lint
       - run: npm run build
```

#### 의사결정 이유

지금은 `generate:api`를 실행해도 generated 결과가 working tree에 남아도 CI가 통과할 수 있습니다.  
이건 OpenAPI drift를 반드시 다시 만들게 됩니다.

---

## 9. `shared-go` package allowlist를 실제 상태에 맞게 갱신하고, stale도 fail 처리

### 문제

현재 실제 non-test package는 아래뿐입니다.

- `envutil`
- `ginjson`
- `httputil`
- `json`
- `jsonutil`
- `logging`
- `runtime/automaxprocs`
- `runtime/lifecycle`
- `stringutil`
- `telemetry`
- `workerpool`

그런데 `docs/architecture/shared-go-package-allowlist.txt`에는 존재하지 않는 package가 많이 남아 있고,  
`check-shared-go-packages.sh`는 이를 단순 info로만 출력합니다.

### 변경 파일
- `docs/architecture/shared-go-package-allowlist.txt`
- `scripts/architecture/check-shared-go-packages.sh`

### allowlist 교체

```diff
diff --git a/docs/architecture/shared-go-package-allowlist.txt b/docs/architecture/shared-go-package-allowlist.txt
@@
-cache
-ctxutil
-dbx
 envutil
-errors
-flagx
 ginjson
-ginserver
-grpcx
-httpclient
 httputil
-httpx
 json
 jsonutil
 logging
-processinglock
-ptrutil
-retry
 runtime/automaxprocs
-shutdown
-sliceutil
+runtime/lifecycle
 stringutil
 telemetry
-textutil
-timeutil
-valkeyx
-valkeyx/mqutil
 workerpool
```

### stale allowlist도 fail

```diff
diff --git a/scripts/architecture/check-shared-go-packages.sh b/scripts/architecture/check-shared-go-packages.sh
@@
 if [[ -n "${new_packages}" ]]; then
   echo "FAIL: new shared-go packages detected (not in allowlist)" >&2
@@
 fi
 
 count="$(wc -l < "${tmp_found}" | tr -d '[:space:]')"
 echo "OK: no new shared-go packages (count: ${count})"
 
 if [[ -n "${stale_allowlist}" ]]; then
-  echo
-  echo "Info: remove stale allowlist entries:"
-  echo "${stale_allowlist}"
+  echo "FAIL: stale shared-go allowlist entries detected" >&2
+  echo "${stale_allowlist}" >&2
+  exit 1
 fi
```

#### 의사결정 이유

allowlist가 SSOT라면 stale entry도 실패해야 합니다.  
지금처럼 새 package만 fail하고 stale는 info만 내면, allowlist는 시간이 갈수록 쓰레기 목록이 됩니다.

---

## 10. 정리되지 않은 추가 문서/설계 drift 수정

### 10-1. `docs/superpowers/plans/2026-03-23-iris-standardization-excluding-game-bot.md`

이 문서는 Task 2 전체가 settlement migration입니다. settlement 제거가 canonical이면 이 문서도 같이 손봐야 합니다.

#### 수정 포인트
- Goal/Architecture paragraph에서 settlement-go 제거
- Task 2 전체 삭제
- Task 번호 재정렬
- verification 섹션에서 `/home/kapu/gemini/settlement-go` 테스트 문구 제거

### 10-2. `docs/history/settlement/README.md` 추가

이력 보존용 폴더를 만든다면, 왜 archive 되었는지 설명하는 index 문서를 같이 두십시오.

```md
# Settlement History

이 디렉터리는 2026-04-09 기준 제거 결정이 내려진 settlement 기능의 historical plans/specs를 보관합니다.

원칙:
- active architecture, build, deploy, command contracts에는 settlement가 존재하지 않습니다.
- 이 디렉터리의 문서는 historical context 보존 목적만 가집니다.
```

#### 의사결정 이유

archive만 해두고 이유를 안 남기면 몇 달 뒤 다시 resurrect 될 수 있습니다.

---

## 11. 검증 순서

### 11-1. Settlement 제거

```bash
go work sync
go build ./shared-go/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-dispatcher-go/... \
  ./hololive/hololive-kakao-bot-go/... \
  ./hololive/hololive-llm-sched/... \
  ./hololive/hololive-stream-ingester/...
```

### 11-2. Architecture gate

```bash
./scripts/architecture/check-removed-runtime-references.sh
./scripts/architecture/check-shared-go-packages.sh
./scripts/architecture/ci-boundary-gate.sh
```

### 11-3. Admin Dashboard

```bash
cd admin-dashboard/backend
cargo test

cd ../frontend
npm ci
npm run generate:api
npm run lint
npm run build
```

### 11-4. Targeted Go tests

```bash
go test ./hololive/hololive-shared/pkg/service/member/...
go test ./hololive/hololive-llm-sched/internal/service/majorevent/summarizer/...
go test ./hololive/hololive-kakao-bot-go/internal/bot/...
```

---

## 12. 적용 순서 권장안

1. **Settlement 제거 1차**: `go.work`, docs, Dockerfile, command/config residue 삭제
2. **Settlement 제거 2차**: migration archive + manual drop runbook + removed-runtime gate
3. **Repo hygiene**: `.dockerignore`, `.gitignore`, export-source-bundle.sh
4. **Thin wrapper 정리**: `applyScraperProxyToggle`, `infraResources`, runtime router helpers
5. **Logging 통일**
6. **Member adapter + major event consensus**
7. **Admin Dashboard backend proxy 제거**
8. **Admin Dashboard frontend/client/CI 정리**
9. **Architecture allowlist strictness**

이 순서가 좋은 이유는, 먼저 “존재하면 안 되는 것”을 지우고, 그 다음 “남아 있는 구조의 품질”을 올리는 순서이기 때문입니다.

---

## 최종 판단

정산을 제거 대상으로 확정하면, 이번 업로드본의 남은 추가 이슈는 단순한 코드 정리가 아니라 **퇴역 설계(decommission design)** 문제로 바뀝니다.  
핵심은 세 가지입니다.

1. settlement를 **active topology에서 완전히 제거**할 것  
2. settlement 데이터는 **자동 삭제가 아니라 수동 runbook으로 퇴역**시킬 것  
3. 다시 들어오지 않게 **architecture gate로 막을 것**

그 위에 남는 thin wrapper, logging SSOT, adapter stub, admin-dashboard transport drift를 같이 닫아야 이번 저장소의 “마지막 AI 냄새”까지 꽤 정리됩니다.
