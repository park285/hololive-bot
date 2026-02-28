# Hololive Small K8s Migration Plan (2026-02-27)

## 목적
- 현재 운영 중인 Hololive 계열 워크로드를 작은 Kubernetes 형태(k3s 단일 노드)로 단계적으로 전환합니다.
- 초기에는 애플리케이션만 이전하여 리스크를 낮추고, 안정화 후 데이터 인프라를 Kubernetes 내부로 이동합니다.

## 범위
- 대상 앱:
  - `hololive-scraper-rs`
  - `hololive-alarm`
  - `hololive-kakao-bot-go`
- 현행 외부 인프라(초기 유지):
  - `holo-postgres` (Docker)
  - `valkey-cache` (Docker)

## 현재 상태 (2026-02-27 기준)
- 앱은 Podman compose로 운영 중이며, 인프라는 Docker compose로 운영 중입니다.
- `hololive-scraper-rs`는 현재 `GET /health` 응답이 정상(200)이고, `/ready`는 런타임별 차이가 있어 probe 경로 정합성 확인이 필요합니다.
- 최근 장애 패턴은 DB 계정 비밀번호 불일치 시 `degraded + 503`로 나타났습니다. 따라서 배포 절차에 DB role/password 동기화 단계를 명시해야 합니다.

---

## Phase 1 (추천, 리스크 낮음)

### 목표
- k3s 단일 노드에 앱만 이전합니다.
- PostgreSQL/Valkey는 기존 Docker를 유지하고 앱이 외부 endpoint로 연결합니다.

### 아키텍처
- k3s:
  - `Deployment`: scraper, alarm, bot
  - `Service`: 각 앱 ClusterIP (필요시 NodePort/Ingress)
  - `ConfigMap/Secret`: 앱 설정과 민감값 분리
- Docker(기존):
  - Postgres, Valkey 지속 운영

### 핵심 설계 포인트
1. 네트워크 연결
- Kubernetes Pod에서 Docker 인프라 endpoint에 접근 가능한 경로를 확보해야 합니다.
- 현재 일부 포트가 `127.0.0.1` 바인딩이면 Pod에서 직접 접근이 불가합니다.
- 선택지:
  - A안: 인프라 포트를 host IP로 노출하고 방화벽/접근제어 적용
  - B안: 앱 Pod에 `hostNetwork: true` 적용(초기 전환은 빠르나 격리 약화)
- 권장: A안(보안/운영 정합성이 높음)

2. 헬스체크 표준화
- Scraper readiness/liveness는 우선 `/health` 기준으로 운영하고, `/ready`를 코드에 복원하는 시점을 별도 태스크로 관리합니다.
- Alarm/Bot은 실제 노출 엔드포인트를 배포 전 재확인합니다.

3. 설정 관리
- `.env` 직접 주입을 중단하고 `ConfigMap + Secret`으로 분리합니다.
- 비밀번호/토큰은 `Secret`, 비민감 런타임 설정은 `ConfigMap`으로 관리합니다.

4. 배포 안전장치
- 롤아웃 전략: `RollingUpdate` (`maxUnavailable=0`, `maxSurge=1`)
- 자원 제한: `requests/limits` 명시
- 장애 시 즉시 기존 Podman compose로 롤백할 수 있도록 이중 운용 기간을 둡니다.

### 실행 순서
1. k3s 설치 및 기본 컴포넌트 확인
2. 네임스페이스 생성 (`hololive`)
3. Secret/ConfigMap 생성
4. 앱 이미지 레지스트리 경로 정리
5. `Deployment/Service` 적용 (scraper -> alarm -> bot 순차)
6. 헬스체크 및 DB/Valkey/OTel 연동 검증
7. 24~72시간 관찰 후 Podman 앱 중지

### 완료 기준 (Acceptance Criteria)
- 24시간 이상 앱 재시작 없이 정상 동작
- Scraper 주기 작업 정상 수행
- Alarm 큐 적재/소비 정상
- Bot API/핵심 기능 정상
- 최근 로그에 아래 오류 패턴 없음:
  - `password authentication failed`
  - `pool timed out`
  - readiness 503 반복

### 롤백
- 앱 트래픽을 즉시 기존 Podman compose로 되돌립니다.
- k3s 워크로드는 scale down 또는 namespace 단위 비활성화합니다.
- 데이터 인프라는 Phase 1에서 변경하지 않으므로 데이터 롤백이 필요 없습니다.

---

## Phase 2 (선택)

### 목표
- PostgreSQL/Valkey까지 Kubernetes 내부로 이동합니다.
- StatefulSet + PVC + backup job 기반으로 운영 체계를 완성합니다.

### 아키텍처
- PostgreSQL:
  - `StatefulSet(1)` + PVC
  - `Service` (ClusterIP)
  - 초기에는 단일 인스턴스, 추후 HA 검토
- Valkey:
  - `StatefulSet(1)` + PVC
  - `Service` (ClusterIP)
### 데이터/복구 전략
1. Postgres backup
- `CronJob`으로 `pg_dump` 또는 base backup 수행
- 백업 저장소(PVC 또는 외부 object storage) 명시
- 주기적 복구 리허설(restore drill) 필수

2. Valkey backup
- AOF/RDB 정책 명시
- `CronJob`으로 스냅샷 아카이빙(필요 시)

3. 운영 문서화
- RPO/RTO 정의
- 장애 시 복구 절차(runbook) 문서화

### 실행 순서
1. Postgres StatefulSet + PVC + backup/restore 검증
2. Valkey StatefulSet + PVC + persistence 검증
3. 모니터링/로그 적재 경로 전환
4. 앱 endpoint를 in-cluster 서비스로 전환
5. Docker 인프라 단계적 종료

### 완료 기준
- 1주 이상 안정 운영
- 백업/복구 리허설 성공
- 앱 로그/메트릭/트레이싱 정상 수집
- Docker 인프라 의존 완전 제거

---

## 리스크 및 대응
1. 네트워크 경로 혼선 (Docker <-> k3s)
- 대응: endpoint/포트/방화벽을 배포 체크리스트에 고정 항목으로 관리

2. DB 인증 불일치 재발
- 대응: 배포 파이프라인에 `ALTER ROLE ... PASSWORD ...` 동기화 단계 포함

3. readiness 경로 불일치
- 대응: 런타임별 실제 엔드포인트를 기준으로 probe를 선언하고, 향후 `/ready` 계약을 코드로 통일

4. 단일 노드 장애
- 대응: 정기 백업 + 롤백 경로 + 복구 리허설로 운영 리스크 완화

---

## 산출물 체크리스트
- `deploy/k8s/base/`:
  - namespace
  - deployment/service (3 apps)
  - configmap/secret 템플릿
- `deploy/k8s/overlays/prod/`:
  - 이미지 태그
  - 리소스 제한
  - 외부 인프라 endpoint
  - probe 경로
- 운영 runbook:
  - 배포
  - 검증
  - 롤백
  - 백업/복구

## 권장 의사결정
- 즉시 실행은 Phase 1만 진행하고, 최소 1주 안정화 데이터 확보 후 Phase 2 착수
- Phase 2 착수 조건은 백업/복구 리허설 자동화 완료로 설정
