# Scripts

`hololive/hololive-api/scripts`는 아래 범주만 유지합니다.

## 1. 로컬 단일 프로세스 보조
- `bot.sh` (`start|stop|restart|rebuild|status`)

## 2. DB 초기화 / 마이그레이션
- `init-db/`
- `migrations/`

운영 배포 진입점은 이 디렉터리가 아니라 **레포 루트** 기준입니다.

- 전체 image 빌드: `./build-all.sh --no-bump --build-only`
- 중앙 배포: `docs/current/runbooks/release.md`(Quadlet, `deploy/quadlet/README.md`)
- PGO 수집·채택: `docs/current/runbooks/pgo.md`
