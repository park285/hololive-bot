# Scripts

루트 `scripts/`는 현재 3개 영역만 운영합니다.

## 1. architecture/
PR/릴리스 전 경계 게이트와 릴리스 노트 렌더링 도구입니다.

- 표준 진입점: `./scripts/architecture/ci-boundary-gate.sh`
- 세부 단계:
  - `m0-gate.sh`
  - `m1-contract-gate.sh`
  - `check-go-module-loc.sh`
  - `m6-gate.sh`

## 2. deploy/
Docker Compose 운영 재배포 스크립트입니다.

- `./scripts/deploy/compose-redeploy-service.sh <service>`

## 3. logs/
Compose 로그 조회/테일/보조 미러링 스크립트입니다.

- `query.sh`
- `tail.sh`
- `backfill.sh`
- `stream.sh`
- `dump.sh`
- `prune.sh`
- `check-outbox-per-room.sh`
- `check-outbox-per-room-cron.sh`

정리 원칙:
- retired/no-op 스크립트는 유지하지 않습니다.
- 운영 표준 진입점은 README와 runbook에 문서화된 것만 남깁니다.
