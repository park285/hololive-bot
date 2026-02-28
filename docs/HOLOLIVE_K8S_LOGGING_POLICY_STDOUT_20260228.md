# Hololive K8s Logging Policy (stdout-first)

- 작성일: 2026-02-28
- 상태: Active
- 범위: `deploy/k8s/hololive/*`, `hololive-scraper-rs/*`

## 1) 목적

Kubernetes 운영 기준에 맞춰 로그 정책을 **stdout-first**로 통일합니다.

- 기본 수집 채널: `kubectl logs`
- 파일 로그는 기본 비활성
- replica 환경에서 파일 충돌/권한/용량 리스크 최소화

## 2) 운영 기본값

### Go 서비스 (hololive-kakao-bot-go 계열)
- 기본: `LOG_DIR=""` (stdout only)

대상:
- `hololive-bot`
- `alarm-dispatcher`
- `stream-ingester`
- `llm-scheduler`
- `admin-api`

### Rust 서비스 (hololive-scraper-rs 계열)
- 기본: `SCRAPER__LOGGING__FILE_ENABLED=false`
- 기본: `ALARM__LOGGING__FILE_ENABLED=false`

대상:
- `hololive-scraper`
- `hololive-alarm`

## 3) 파일 로그 사용(예외 모드)

기본은 stdout-only이며, 필요 시에만 파일 로그를 활성화합니다.

- Scraper: `SCRAPER__LOGGING__FILE_ENABLED=true`
- Alarm: `ALARM__LOGGING__FILE_ENABLED=true`

주의:
- 파일 모드 사용 시 회전 정책(앱 내 또는 외부 logrotate)을 반드시 함께 운영합니다.
- K8s prod 상시 운영은 stdout-only를 권장합니다.

## 4) 스토리지/권한 정책

- 로그 전용 hostPath(``/srv/hololive/logs``)는 기본 사용하지 않습니다.
- 데이터 경로만 유지:
  - `/srv/hololive/data`
  - 권장 권한/소유자: `0770`, `uid=1000`, `gid=1000`

예시:
```bash
sudo install -d -m 0770 -o 1000 -g 1000 /srv/hololive/data
```

## 5) 검증 절차

1. 매니페스트 렌더링:
```bash
kubectl kustomize deploy/k8s/hololive/overlays/prod >/tmp/hololive-kustomize.yaml
```

2. 로그 확인:
```bash
kubectl -n hololive logs deploy/hololive-bot --tail=200
kubectl -n hololive logs deploy/hololive-scraper --tail=200
kubectl -n hololive logs deploy/hololive-alarm --tail=200
```

3. 확인 포인트:
- `file_logging_enabled stdout_only=true` 로그 존재
- 파일 권한/마운트 오류 미발생
- readiness/liveness 반복 실패 없음

## 6) 비범위(이번 변경 제외)

- Fluent Bit / Loki / ELK 등 중앙집중 로그 스택 도입

