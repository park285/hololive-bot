# Compose History Notes (2026-03-07)

k8s/k3s 운영에서 Docker Compose 기준으로 회귀할 때 git history에서 확인한 주요 설정 메모입니다.

## 핵심 결론

현재 복구한 compose 스택은 **`3db5ac9` 시점**과 가장 가깝습니다.

- `3db5ac9` `chore(ops): switch deploy/docs to 4-service go topology`
  - 서비스: `hololive-bot`, `dispatcher-go`, `stream-ingester`, `llm-scheduler`
  - 보조: `holo-postgres`, `valkey-cache`, `hololive-db-migrate`, `docker-proxy`, `deunhealth`
  - 현재 운영 회귀 기준으로 가장 적합

현재 운영 기준:
- 배포: `./build-all.sh --no-bump`
- 단일 서비스 재배포: `./scripts/deploy/compose-redeploy-service.sh <service>`
- 상태 확인: `docker compose -f docker-compose.prod.yml ps`
- 로그 SSOT: `docker compose -f docker-compose.prod.yml logs`

## 서비스 토폴로지 변화

### 1) `1da02d2` — 초기 compose 기반 Go 이관 시점
- 서비스:
  - `hololive-bot`
  - `alarm-dispatcher`
  - `stream-ingester`
  - `llm-scheduler`
  - `admin-api`
- 특징:
  - `ALARM_DISPATCHER_URL=http://alarm-dispatcher:30010`
  - `admin-api`가 별도 컨테이너였음
  - bot/ingester 모두 queue consumer 분리를 전제로 동작

### 2) `6a2d3a6` — Rust dispatcher 잔존 시점
- 서비스:
  - `hololive-bot`
  - `rust-dispatcher`
  - `stream-ingester`
  - `llm-scheduler`
  - `admin-api`
- 특징:
  - `ALARM_DISPATCHER_URL=http://hololive-bot:30001`로 내부 CRUD 경로가 bot으로 회귀
  - `admin-api`는 아직 별도 컨테이너

### 3) `3db5ac9` — 4-service compose 최종형
- 서비스:
  - `hololive-bot`
  - `dispatcher-go`
  - `stream-ingester`
  - `llm-scheduler`
- 특징:
  - `admin-api`가 bot 런타임으로 흡수된 이후 형태
  - `ALARM_DISPATCHER_URL=http://hololive-bot:30001`
  - `LLM_SCHEDULER_INTERNAL_URL=http://llm-scheduler:30003`
  - compose용 `build-all.sh`가 아직 활성 상태

## 설정적으로 유지된 공통점

여러 compose 시점에서 공통적으로 유지된 설정:

- PostgreSQL: `host.docker.internal:5433`
- Valkey: `valkey-cache:6379` + `/var/run/valkey/valkey-cache.sock`
- 앱 포트:
  - bot `30001`
  - llm-scheduler `30003`
  - stream-ingester `30004`
- infra/support:
  - `docker-proxy`
  - `deunhealth`
- 대부분의 앱 컨테이너는 `127.0.0.1` loopback publish

## 회귀 시 참고 포인트

1. 가장 최근의 안정 compose 기준은 `3db5ac9`
2. 그 이전 compose들은 `admin-api`, `alarm-dispatcher`, `rust-dispatcher` 등 이미 정리된 런타임을 포함
3. 따라서 현재 compose 회귀는 **`3db5ac9` 기준 + 현재 코드 구조(bot에 admin API 통합)** 조합이 맞음
4. 운영 문서 SSOT는 `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`
