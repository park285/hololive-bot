# Hololive Bot

홀로라이브(Hololive) 버튜버 방송 알림 및 종합 운영 관리 플랫폼입니다.

카카오톡(KakaoTalk) 챗봇을 통하여 실시간 방송 알림, 방송 스트리밍 상태 조회, 멤버 소식 정보 제공 및 시스템 원격 관리 기능을 처리합니다.

본 문서는 저장소의 메인 가이드라인입니다. 시스템 설계 및 모듈 간 아키텍처 상세 사양은 [PROJECT_MAP.md](docs/current/PROJECT_MAP.md) 문서를, 상세 서비스 배포 절차는 [DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md](docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md) 가이드를 최우선 사양(SSOT)으로 삼습니다.

---

## 시스템 아키텍처 (Architecture Overview)

본 플랫폼은 Go 기반으로 구현된 **3개의 독립된 애플리케이션 런타임 서비스**(`hololive-api`, `alarm-worker`, `youtube-producer`)로 구성되어 있으며, 단일 호스트 상에서 Docker Compose를 통해 격리 가동됩니다. `hololive-api`는 bot/admin/llm plane을 단일 프로세스에서 호스팅합니다. (단, youtube-producer 컴포넌트는 부하 분산 및 신뢰성을 위해 Seoul 및 메인 호스트에 걸쳐 2-way Active-Active 형태로 확장 운용됩니다.)

인프라 이력 사양: 이전의 k8s/k3s 오케스트레이션 구성에서 관리 편의성 향상을 위해 단일 호스트 Docker Compose 기반 환경으로 롤백 복귀하였습니다. 현재 배포 롤아웃 및 로그 분석, 트러블슈팅의 표준 준거는 Docker Compose 운영 문서군을 따릅니다.

### 런타임 컴포넌트 일람 (Runtime Services)

| 런타임 모듈명 | 소스 디렉토리 | Compose 서비스명 | 가동 포트 | 주요 역할 |
|---|---|---|---:|---|
| hololive-api | hololive-api | hololive-api | 30001/30003/30006 | bot/admin/llm plane 통합 런타임: Kakao/Iris 웹훅 라우팅·챗 명령어 파싱(bot), 관리자 API 컨트롤 플레인(admin), 이벤트/뉴스 정규화 및 LLM 스케줄러(llm) |
| alarm-worker | hololive-alarm-worker | hololive-alarm-worker | 30007 | 방송 정보 주기적 분석, 발송 대기열 소비 및 Iris outbound 호출 |
| youtube-producer | hololive-youtube-producer | youtube-producer | 30015/25 | 유튜브 채널 모니터링, 신규 정보 발행 및 액티브-액티브 제어 |

### 📦 공유 라이브러리 (Shared Libraries)

* **hololive-shared:** 홀로라이브 도메인 도큐먼트, 데이터 스키마 및 공통 비즈니스 로직 정의
* **shared-go:** 로그 처리, 트레이싱 등을 처리하는 범용 Go 공통 모듈 (Submodule 연계)

### 🔄 핵심 데이터 흐름 (Core Flow)

* 인바운드 메시지 처리: `Iris Core -> hololive-api (bot plane) -> Command/Service -> PostgreSQL & Valkey`
* 실시간 알림 발송: `alarm-worker -> Valkey (alarm:dispatch:queue) -> alarm-worker egress -> Iris Core -> 카카오톡`
* LLM 뉴스 분석 연계: `hololive-api` admin/bot plane 내장 클라이언트 -> `hololive-api` (llm plane) 내부 API
* 유튜브 신작 감지: `youtube-producer -> Shared Outbox/Tracking DB -> alarm-worker`

---

## 개발 및 검증 (Development & Test)

### 사전 요구 사양 (Prerequisites)

* Go 1.26 계열
* PostgreSQL (영속 저장소)
* Valkey (세션 및 큐)
* Docker Compose (로컬 개발 및 통합 검증용)

### 1. 전체 소스 코드 컴파일 (Build)

```bash
go work sync
go build ../shared-go/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-api/... \
  ./hololive/hololive-alarm-worker/... \
  ./hololive/hololive-youtube-producer/...
```

### 2. 전체 단위 테스트 실행 (Test)

```bash
go test ../shared-go/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-api/... \
  ./hololive/hololive-alarm-worker/... \
  ./hololive/hololive-youtube-producer/...
```

* 독립 모듈 규격 검사: `go test . -run TestRuntimeSplitStandaloneModulesContract`
* 아키텍처 가드레일 정적 검사:
  ```bash
  ./scripts/architecture/check-project-map.sh
  ./scripts/architecture/ci-boundary-gate.sh
  ```
* 로컬 통합 품질 게이트: `./scripts/ci/local-ci.sh`

배포 스크립트(`./build-all.sh`) 기동 시, Docker 이미지 빌드 단계 진입 전에 `local-ci.sh` 품질 게이트가 자동으로 선행 수행됩니다. 만약 해당 품질 검사(린트, NilAway, 경합 테스트, staticcheck, 취약점 진단 등) 중 하나라도 실패할 경우 빌드 프로세스가 강제 차단됩니다.

---

## 운영 배포 절차 (Deployment)

운영계 배포 및 롤아웃은 아래 배포 스크립트 도구를 이용하여 모듈별로 안전하게 진행됩니다.

```bash
# 전체 산출물 컴파일 진행 (버전 범프 제외 옵션)
./build-all.sh --no-bump

# 특정 개별 서비스 단위 컨테이너 롤아웃 재배포
./scripts/deploy/compose-redeploy-service.sh hololive-api
./scripts/deploy/compose-redeploy-service.sh hololive-alarm-worker

# 유튜브 프로듀서 Active-Active 멀티 프로파일 재배포
COMPOSE_FILE=deploy/compose/docker-compose.prod.yml:deploy/compose/docker-compose.main-ap.yml \
COMPOSE_PROFILES=main-ap \
./scripts/deploy/compose-redeploy-service.sh youtube-producer-c
```

원격 인프라 노드(`youtube-producer-b`) 배포에 대한 매뉴얼은 [DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md](docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md) 내부의 `./scripts/deploy/ap-deploy.sh <host>` 운영 절차를 준수해 주십시오.

---

## 서비스 상태 진단 (Logs & Health Check)

* 로그 분석 표준(SSOT): 개발 및 문제 원인 추적 시 각 서비스의 표준 출력/표준 에러(stdout/stderr) 및 Docker Compose의 raw 로그 출력을 1순위 데이터로 다룹니다.
* 로컬 파일 로그: 디버깅 시 참고용 보조 미러 정보로만 취급합니다.

### 서비스별 상태 진단 엔드포인트

| 런타임 모듈 | 상태 검증 URI (Health Check) |
|---|---|
| hololive-api (bot) | `https://127.0.0.1:30001/health` via container `./bin/healthcheck` |
| hololive-api (llm) | `https://127.0.0.1:30003/health` via container `./bin/healthcheck` |
| hololive-api (admin) | `https://127.0.0.1:30006/health` via container `./bin/healthcheck` |
| alarm-worker | `https://127.0.0.1:30007/health` via container `./bin/healthcheck` |
| youtube-producer | `https://127.0.0.1:30025/health` via container `./bin/healthcheck` (Main 노드) |

```bash
# Docker Compose 컨테이너 동작 상태 점검
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml ps

# 실시간 컨테이너 로그 분석
./scripts/deploy/compose.sh -f deploy/compose/docker-compose.prod.yml logs -f <service_name>

# 특정 시점 범위의 로그 필터링 쿼리
./scripts/logs/logs.sh query <service_name> --since 1h --limit 1000
```

---

## 연관 기술 문서 (Related Documentation)

* [**docs/README.md**](docs/README.md) - 전체 기술 문서 보관용 인덱스
* [**docs/current/PROJECT_MAP.md**](docs/current/PROJECT_MAP.md) - 현 시점의 전체 런타임 및 아키텍처 인벤토리 맵 (SSOT)
* [**docs/current/SERVICE_OWNERSHIP.md**](docs/current/SERVICE_OWNERSHIP.md) - 서비스 모듈별 관리 소유권 명세
* [**docs/current/CONTRACT_MAP.md**](docs/current/CONTRACT_MAP.md) - 각 컴포넌트 간 통신 규격 프로토콜 계약 지도
