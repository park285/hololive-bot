# Hololive K8s Logging Policy (stdout + file)

- 작성일: 2026-02-28
- 개정일: 2026-03-01
- 상태: Active
- 범위: `k8s/*`, `hololive/hololive-scraper-rs/*`

## 1) 목적

Kubernetes 운영 기준에서 `kubectl logs`와 호스트 파일 로그를 함께 운영합니다.

- 기본 수집 채널: `kubectl logs`
- 파일 로그 경로: `/srv/hololive/logs` (단일 디렉터리)
- 장애 시 빠른 현장 확인(호스트) + 중앙 수집(Loki) 병행

## 2) 운영 기본값

### Go 서비스 (hololive-kakao-bot-go 계열)
- 기본: `LOG_DIR=/srv/hololive/logs`
- 기본: `LOG_FILE=<service>.log`

대상:
- `hololive-bot`
- `alarm-dispatcher`
- `stream-ingester`
- `llm-scheduler`
- `admin-api`

### Rust 서비스 (hololive-scraper-rs 계열)
- 기본: `SCRAPER__LOGGING__FILE_ENABLED=true`
- 기본: `ALARM__LOGGING__FILE_ENABLED=true`
- 기본: `SCRAPER__LOGGING__DIR=/srv/hololive/logs`
- 기본: `ALARM__LOGGING__DIR=/srv/hololive/logs`
- 기본: `*_LOGGING__COMBINED_FILE`은 서비스별 파일명으로 분리

## 3) 스토리지/권한 정책

- 데이터 경로: `/srv/hololive/data`
- 로그 경로: `/srv/hololive/logs`
- 권장 권한/소유자: `0770`, `uid=1000`, `gid=1000`

예시:
```bash
sudo install -d -m 0770 -o 1000 -g 1000 /srv/hololive/data
sudo install -d -m 0770 -o 1000 -g 1000 /srv/hololive/logs
```

## 4) 검증 절차

1. 매니페스트 렌더링:
```bash
kubectl kustomize k8s/overlays/prod --enable-helm >/tmp/hololive-kustomize.yaml
```

2. 로그 확인:
```bash
kubectl -n hololive logs deploy/hololive-bot --tail=200
kubectl -n hololive logs deploy/hololive-scraper --tail=200
kubectl -n hololive logs deploy/hololive-alarm --tail=200
```

3. 호스트 파일 로그 확인:
```bash
sudo ls -lh /srv/hololive/logs
sudo find /srv/hololive/logs -maxdepth 2 -type f | head
```

4. 확인 포인트:
- 파일 권한/마운트 오류 없음
- readiness/liveness 반복 실패 없음
- 로그 파일 생성/증분 확인
