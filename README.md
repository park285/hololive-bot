# Hololive Bot

홀로라이브 VTuber 알림/관리 플랫폼. KakaoTalk 챗봇을 통해 방송 알림, 스트림 상태, 멤버 뉴스 등을 제공합니다.

## 아키텍처

Go 단일 언어 구조:

| 영역 | 언어 | 역할 |
|------|------|------|
| Runtime | Go | bot(+admin API), dispatcher-go, llm-scheduler, stream-ingester |

데이터 흐름: `webhook → handler → service → repository → PostgreSQL/Valkey`

알림 흐름: `bot(alarm scheduler) LPUSH alarm:dispatch:queue → dispatcher-go BRPOP → Iris(Redroid) → KakaoTalk`

## 모듈 구조

### Go 모듈 (6개, go.work: 런타임 4 + 라이브러리 2)
| 모듈 | 역할 | 포트 |
|------|------|------|
| `hololive-kakao-bot-go` | Main bot (webhook + command routing + admin API) | 30001 |
| `hololive-dispatcher-go` | Alarm dispatch consumer (BRPOP → Iris) | 30020 |
| `hololive-llm-sched` | LLM scheduler (major event + member news + delivery) | 30003 |
| `hololive-stream-ingester` | YouTube/Holodex/Chzzk/Twitch polling + stats | 30004 |
| `hololive-shared` | Shared Go library (hololive domain) | - |
| `shared-go` | Shared Go utilities (errors, stringutil, valkeyx, etc.) | - |

### 인프라
| 항목 | 설명 |
|------|------|
| PostgreSQL | 메인 데이터베이스 (Docker) |
| Valkey | 캐시/큐 (k3s) |
| k3s | Go 서비스 배포 (bot, dispatcher-go, llm-scheduler, stream-ingester) |
| Iris (Redroid) | KakaoTalk 자동화 |

상세: `docs/PROJECT_MAP.md`

## 개발

### 사전 조건
- Go 1.26+
- PostgreSQL, Valkey

### 빌드
```bash
# Go (workspace 기준)
go work sync
go build ./hololive/hololive-kakao-bot-go/... \
  ./hololive/hololive-dispatcher-go/... \
  ./hololive/hololive-llm-sched/... \
  ./hololive/hololive-stream-ingester/... \
  ./hololive/hololive-shared/... \
  ./shared-go/...
```

### 테스트
```bash
# Go (workspace 주요 모듈)
go test ./hololive/hololive-kakao-bot-go/... \
  ./hololive/hololive-dispatcher-go/... \
  ./hololive/hololive-llm-sched/... \
  ./hololive/hololive-stream-ingester/... \
  ./hololive/hololive-shared/... \
  ./shared-go/...
```

## 배포

k3s 기반. 상세 배포 가이드: `docs/runbook_execution/K3S_DEPLOYMENT_GUIDE.md`

```bash
# 기본 배포
kubectl kustomize k8s/overlays/prod --enable-helm | kubectl apply --server-side -f -
```

### 로그 정책
- SSOT: **stdout → Fluent Bit → Loki** 단일 경로
- Grafana: `http://localhost:30090`
- CLI: `./scripts/logs/tail.sh <service>`, `./scripts/logs/query.sh <service>`
- 서비스 키: `bot`, `dispatcher`(=`dispatcher-go`), `llm`(=`llm-scheduler`), `ingester`(=`stream-ingester`)

## 문서

- `docs/README.md` — 문서 인덱스
- `docs/PROJECT_MAP.md` — 모듈 구조
- `docs/NEXT_TODO.md` — 향후 작업
- `docs/architecture/` — 아키텍처 결정 기록
- `docs/modularization/` — 모듈화 계획

## Phase 11 진행 스냅샷 (2026-03-05, 라운드13 기준)

- B7 테스트 로거 통합: 로컬 `testLogger/newTestLogger` 정의 0건 확인
  - `rg -n "func (testLogger|newTestLogger)" -g '*_test.go' hololive shared-go`
- D1 체커 커버리지: `go test -count=1 -cover ./internal/service/alarm/checker/...` → **82.7%**
- B8 miniredis 헬퍼 치환: 4개 대상 중 3개 완료, `hololive-shared/pkg/service/cache/service_test.go` 잔여
- D2 알림 테스트/커버리지: table-driven 시나리오 확장 + `go test -count=1 -cover ./internal/service/notification/...` → **76.6%**
- D4 ingester(app) 테스트/커버리지: 런타임 precondition/infra-failure 회귀 + `go test -count=1 -cover ./internal/app/...` → **62.0%**
- 잔여 핵심: **B8(cache/service_test miniredis 헬퍼), D1(youtube table-driven 5개), D4(runtime_builder 정상 빌드 시나리오)**

상세 체크리스트/검증 로그: `docs/modularization/TODO_PHASE11.md`
