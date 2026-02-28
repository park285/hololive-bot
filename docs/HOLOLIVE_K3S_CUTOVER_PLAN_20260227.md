# Hololive k3s Cutover Plan (Game-bot 제외, Postgres 외부, Valkey 내부)

- 작성일: 2026-02-27
- 상태: Draft v1
- 대상 환경: 단일 노드 k3s (prod)

## 1) 목표

기존 `docker-compose.prod.yml`에서 **Hololive 계열 애플리케이션만** k3s로 이전합니다.

- 포함: `hololive-bot`, `alarm-dispatcher`, `stream-ingester`, `llm-scheduler`, `admin-api`, `valkey-cache`
- 제외: `game-bot-go` 계열 전체
- DB 정책: `holo-postgres`는 외부(기존 Docker/호스트) 유지

## 2) 범위 정의

### In Scope
- Hololive 앱 Deployment/Service 구성
- Valkey(내부 Redis) Deployment/Service 구성
- Secret/ConfigMap 분리
- 운영 runbook(배포/검증/롤백) 확정

### Out of Scope
- Game bot/mcp-llm 서비스의 k3s 이전
- PostgreSQL의 StatefulSet/PVC 내부화
- Ingress/TLS 전체 전환(필요 시 별도 단계)

## 3) 현재 구성에서의 핵심 전환 포인트

1. Compose의 `depends_on` 제거
   - Kubernetes의 `readinessProbe/livenessProbe`로 대체
2. Unix socket 경로 의존 제거
   - `CACHE_SOCKET_PATH` 대신 `CACHE_HOST=valkey-cache`, `CACHE_PORT=6379` 사용
3. 외부 의존 endpoint 명시
   - Postgres/Redroid/Cliproxy/mcp-llm/game-bot health URL을 ConfigMap으로 관리
4. 민감정보 분리
   - DB password, token, API key는 Secret으로 분리

## 4) 목표 아키텍처

- Namespace: `hololive`
- 내부 서비스:
  - `valkey-cache` (ClusterIP:6379)
  - `hololive-bot` (ClusterIP:30001)
  - `alarm-dispatcher` (ClusterIP:30010)
  - `stream-ingester` (ClusterIP:30004)
  - `llm-scheduler` (ClusterIP:30003)
  - `admin-api` (ClusterIP:30002)
- 외부 연동:
  - Postgres (`POSTGRES_HOST:POSTGRES_PORT`)
  - Redroid (`IRIS_BASE_URL`)
  - Cliproxy (`CLIPROXY_BASE_URL`)
  - mcp-llm / game-bot health endpoints

## 5) 실행 단계 (Milestone)

### M0. 사전 점검
- k3s 노드 자원 확인 (CPU/Memory)
- 외부 Postgres 네트워크 접근 가능 확인
- 이미지 태그/레지스트리 확인

### M1. 구성 고정
- `deploy/k8s/hololive/base/configmap-app.yaml` 실환경 값 반영
- `deploy/k8s/hololive/base/secret-app.yaml` 실환경 값 반영
- hostPath 디렉터리(`/srv/hololive/data`) 준비

### M2. 1차 배포
- 순서: `valkey-cache` → `alarm-dispatcher` → `llm-scheduler` → `stream-ingester` → `hololive-bot` → `admin-api`
- 각 단계에서 readiness/liveness, 로그 에러 여부 확인

### M3. 기능 검증
- 주요 API/스케줄러/큐 소비 검증
- DB 연결 에러, auth 실패, timeout 반복 여부 확인

### M4. 안정화
- 24~72시간 모니터링
- 장애 패턴 없으면 cutover 완료 선언

## 6) 수용 기준 (Acceptance Criteria)

1. `kubectl -n hololive get pods` 결과 모든 Pod가 `Running` + `Ready(1/1)`
2. 핵심 앱 health endpoint 응답 정상(HTTP 200)
3. 로그에 아래 오류가 30분 이상 반복되지 않음
   - `password authentication failed`
   - `pool timed out`
   - readiness probe 실패 반복
4. 알람/스케줄러/ingestion 핵심 기능 동작 확인
5. 롤백 절차가 실제로 1회 리허설되어 성공

추가 기준:
- K8s prod 로그는 stdout-only 정책으로 운영 (`kubectl logs` 기준)

## 7) 리소스 계획

- hololive + internal valkey memory limit 합: 약 **2.13GiB**
- k3s/system 오버헤드 포함 예상: 약 **3.1~4.1GiB**
- 현재 서버(54Gi RAM, 12 vCPU) 기준 메모리 병목 가능성은 낮음

## 8) 리스크와 대응

1. 외부 Postgres 접근 불가
- 대응: 배포 전 Pod에서 TCP connect 점검, 방화벽 룰 사전 반영

2. 외부 서비스 host명이 Pod에서 해석되지 않음
- 대응: ConfigMap에 IP/FQDN 고정값 사용

3. Secret 값 누락
- 대응: 배포 전 키 유효성 체크 스크립트/체크리스트 운영

4. Valkey 장애 시 앱 연쇄 실패
- 대응: valkey 선배포 + health green 이후 앱 순차 배포

## 9) 롤백 전략

- `kubectl delete -k deploy/k8s/hololive/overlays/prod`
- 기존 Docker Compose Hololive 앱 재기동
- DB는 외부 유지 정책이므로 데이터 롤백은 불필요

## 10) 관련 산출물

- 매니페스트: `deploy/k8s/hololive/`
- 운영 요약: `deploy/k8s/hololive/README.md`
- 실행 절차: `docs/runbook_execution/HOLOLIVE_K3S_CUTOVER_RUNBOOK_20260227.md`
