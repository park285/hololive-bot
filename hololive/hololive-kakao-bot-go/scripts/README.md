# Scripts

`hololive-kakao-bot-go/scripts`는 아래 3개 범주만 유지합니다.

## 1. 로컬 단일 프로세스 보조
- `bot.sh` (`start|stop|restart|rebuild|status`)

## 2. 선택적 빌드 실험
- `build-with-pgo.sh`

## 3. DB 초기화 / 마이그레이션
- `init-db/`
- `migrations/`

운영 배포 진입점은 이 디렉터리가 아니라 **레포 루트** 기준입니다.

- 전체 스택: `./build-all.sh --no-bump`
- 단일 서비스 재배포: `./scripts/deploy/compose-redeploy-service.sh <service>`
