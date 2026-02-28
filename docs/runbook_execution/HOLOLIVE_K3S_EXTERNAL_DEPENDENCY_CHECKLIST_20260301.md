# Hololive k3s 외부 의존성 드리프트 체크리스트

- 작성일: 2026-03-01
- 범위: `k8s/overlays/prod`(DNS ExternalName), `k8s/overlays/prod-ip`(EndpointSlice)
- 목적: 배포 전 `externalName/port` 및 ConfigMap endpoint 드리프트를 자동/수동 점검

## 1) 자동 검증 (필수 게이트)

### 1-1. DNS ExternalName 모드
```bash
python3 k8s/scripts/validate_external_dependencies.py --overlay k8s/overlays/prod --mode dns
```

### 1-2. IP EndpointSlice 모드
```bash
python3 k8s/scripts/validate_external_dependencies.py --overlay k8s/overlays/prod-ip --mode ip
```

PASS 기준:
- [ ] 두 명령 모두 exit code 0
- [ ] `[PASS] external dependency validation succeeded` 출력 확인

## 2) 수동 체크 (변경 시 필수)

### 2-1. ExternalName 대상 재확인 (`prod`)
- [ ] `k8s/overlays/prod/patch-external-services-to-externalname.yaml`의 `externalName`이 최신 대상과 일치
- [ ] 컨테이너 재생성/재할당이 잦은 대상(`llm-server`, `game-bot-*`) 변경 여부 확인

### 2-2. EndpointSlice 대상 재확인 (`prod-ip`)
- [ ] `k8s/base/postgres-external-service.yaml`, `k8s/base/host-external-services.yaml`의 `endpoints.addresses` 최신화
- [ ] 서비스별 port가 ConfigMap URL/health URL 포트와 일치

### 2-3. ConfigMap endpoint 정합성
- [ ] `POSTGRES_HOST/POSTGRES_PORT`가 `holo-postgres:5433` 유지
- [ ] 아래 key host/port가 서비스 정의와 일치
  - `IRIS_BASE_URL`
  - `CLIPROXY_BASE_URL`
  - `SERVICES_LLM_SERVER_HEALTH_URL`
  - `SERVICES_GAME_BOT_TWENTYQ_HEALTH_URL`
  - `SERVICES_GAME_BOT_TURTLE_HEALTH_URL`

## 3) 배포 전 최종 승인

아래 조건을 모두 만족하면 배포 진행 가능:
- [ ] 자동 검증 PASS
- [ ] 외부 대상 변경 이력(무엇/왜/언제) 기록
- [ ] 롤백 모드(`prod ↔ prod-ip`) 선택 및 담당자 확인
