# Hololive Bot

홀로라이브 VTuber 알림/관리 플랫폼. KakaoTalk 챗봇을 통해 방송 알림, 스트림 상태, 멤버 뉴스 등을 제공합니다.

## 아키텍처

Go 단일 언어 구조:

> 운영 기준 (2026-03-07): 기존 k8s/k3s 배포에서 **단일 호스트 Docker Compose 기준으로 롤백**했습니다. 현재 배포, 로그 조회, 장애 대응 절차의 SSOT는 compose 문서입니다.

| 영역 | 언어 | 역할 |
|------|------|------|
| Runtime | Go | bot(+admin API), dispatcher-go, llm-scheduler, stream-ingester, youtube-scraper |

데이터 흐름: `webhook → handler → service → repository → PostgreSQL/Valkey`

알림 흐름: `bot(alarm scheduler) LPUSH alarm:dispatch:queue → dispatcher-go BRPOP → Iris(Redroid) → KakaoTalk`

## 모듈 구조

### Go 모듈 (6개, go.work: 런타임 4 + 라이브러리 2)
| 모듈 | 역할 | 포트 |
|------|------|------|
| `hololive-kakao-bot-go` | Main bot (webhook + command routing + admin API) | 30001 |
| `hololive-dispatcher-go` | Alarm dispatch consumer (BRPOP → Iris) | 30020 |
| `hololive-llm-sched` | LLM scheduler (major event + member news + delivery) | 30003 |
| `hololive-stream-ingester` | Photo sync + ingestion-adjacent runtime builders (`stream-ingester`, `youtube-scraper`) | 30004 / 30005 |
| `hololive-shared` | Shared Go library (hololive domain) | - |
| `shared-go` | In-repo shared Go utilities workspace (`shared-go/`) | - |

### Runtime 바이너리 (5개)
| 바이너리 | 역할 | 포트 |
|------|------|------|
| `bot` | Main bot (+ admin API) | 30001 |
| `dispatcher-go` | Alarm dispatch consumer | 30020 |
| `llm-scheduler` | LLM scheduler | 30003 |
| `stream-ingester` | Photo sync + ingestion-adjacent health/config runtime | 30004 |
| `youtube-scraper` | YouTube polling/scraping + outbox runtime | 30005 |

현재 `docker-compose.prod.yml` 운영 스택은 `bot`, `dispatcher-go`, `llm-scheduler`, `stream-ingester`, `youtube-scraper` 5개 서비스 기준입니다.

### 인프라
| 항목 | 설명 |
|------|------|
| PostgreSQL | 메인 데이터베이스 (Docker) |
| Valkey | 캐시/큐 (Docker) |
| Docker Compose | 현재 운영 Go 서비스 배포 (bot, dispatcher-go, llm-scheduler, stream-ingester, youtube-scraper) |
| Iris (Redroid) | KakaoTalk 자동화 |

상세: `docs/current/PROJECT_MAP.md`

## 개발

### 사전 조건
- Go 1.26.3
- PostgreSQL, Valkey

### 빌드
```bash
# Go (workspace 기준)
go work sync
go build ./shared-go/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-dispatcher-go/... \
  ./hololive/hololive-kakao-bot-go/... \
  ./hololive/hololive-llm-sched/... \
  ./hololive/hololive-stream-ingester/...
```

### 테스트
```bash
# Go (workspace 주요 모듈)
go test ./shared-go/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-dispatcher-go/... \
  ./hololive/hololive-kakao-bot-go/... \
  ./hololive/hololive-llm-sched/... \
  ./hololive/hololive-stream-ingester/...
```

## 배포

현재 운영 기준은 Docker Compose 기반입니다. 상세 배포 가이드: `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`

```bash
# 전체 스택 기동/빌드
./build-all.sh --no-bump

# 단일 compose 서비스 재배포
./scripts/deploy/compose-redeploy-service.sh hololive-bot
./scripts/deploy/compose-redeploy-service.sh dispatcher-go
./scripts/deploy/compose-redeploy-service.sh llm-scheduler
./scripts/deploy/compose-redeploy-service.sh stream-ingester
./scripts/deploy/compose-redeploy-service.sh youtube-scraper
```

### 로그 정책
- SSOT: **application stdout/stderr → `docker compose logs`**
- 파일 미러링: `./logs/bot.log`, `./logs/dispatcher-go.log`, `./logs/llm-scheduler.log`, `./logs/stream-ingester.log`, `./logs/youtube-scraper.log`
- 앱 파일 로그 로테이션: **100MB × 5 backups × 30일 보관 × gzip 압축**
- 압축 백업 보관: `./logs/archive/*.gz`
- 컨테이너 로그 드라이버(`json-file`) 로테이션: **10MB × 3 files**
- 보조 로그 디렉터리:
  - 기본 운영 경로는 `logs/*.log`와 `logs/archive/*.gz`
  - `logs/mirror/`, `logs/backfill/`, `logs/canary/`, `logs/cron/`, `logs/runtime/pids/`는 opt-in 보조 산출물
- 기본 확인: `docker compose -f docker-compose.prod.yml logs -f <service>`
- 범위 조회: `./scripts/logs/logs.sh query <service> --since 1h --limit 1000`
- 실시간 tail: `./scripts/logs/logs.sh tail <service> --since 30m`
- 일회성 스냅샷: `ENABLE_LOG_AUX_FILES=1 ./scripts/logs/logs.sh backfill <service> --since 24h`
- 선택적 로컬 미러링: `ENABLE_LOG_MIRROR=1 ./scripts/logs/logs.sh stream start` 또는 `ENABLE_LOG_MIRROR=1 ./scripts/logs/logs.sh dump`
- 보조 로그 정리: `./scripts/logs/logs.sh prune`
- 상태 확인: `docker compose -f docker-compose.prod.yml ps`
- Health endpoint: `bot(30001)`, `dispatcher-go(30020)`, `llm-scheduler(30003)`, `stream-ingester(30004)`, `youtube-scraper(30005)`

## 문서

- `docs/README.md` — 문서 인덱스
- `docs/current/PROJECT_MAP.md` — 모듈 구조
- `docs/20260307/COMPOSE_HISTORY_NOTES_20260307.md` — compose 회귀 기준 메모
