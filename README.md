# Hololive Bot

홀로라이브(Hololive) 버튜버 방송 알림 및 종합 운영 관리 플랫폼입니다.

카카오톡(KakaoTalk) 챗봇으로 실시간 방송 알림, 방송 스트리밍 상태 조회, 멤버 소식 제공, 시스템 원격 관리 기능을 처리합니다.

본 문서는 저장소의 메인 가이드라인입니다. 시스템 설계 및 모듈 간 아키텍처 상세 사양은 [PROJECT_MAP.md](docs/current/PROJECT_MAP.md)를, 서비스별 배포·복구 절차는 [Current Runbooks](docs/current/runbooks/README.md)를 최우선 사양(SSOT)으로 삼습니다.

---

## 시스템 아키텍처 (Architecture Overview)

본 플랫폼은 Go 기반으로 구현된 **3개의 독립된 애플리케이션 런타임 서비스**(`hololive-api`, `alarm-worker`, `youtube-collector`)로 구성됩니다. 중앙 호스트의 `hololive-api`, `alarm-worker`, collector `c`는 rootful Podman Quadlet([deploy/quadlet](deploy/quadlet/README.md))으로, Seoul collector `b`는 iris-seoul rootful Podman Quadlet([deploy/podman/iris-seoul](deploy/podman/iris-seoul/README.md))으로 실행합니다. Osaka collector `a`와 Osaka2 collector `d`는 host-native systemd로 실행합니다. `hololive-api`는 bot/admin plane과 YouTube consume plane을 단일 프로세스에서 호스팅합니다.

현재 배포 대상과 실행 방식은 [PROJECT_MAP](docs/current/PROJECT_MAP.md)과 [Deployment Baseline](docs/current/DEPLOYMENT_BASELINE.md), 작업 절차는 [Current Runbooks](docs/current/runbooks/README.md)를 따릅니다. 과거 기록과 설계 문서의 전환 절차를 현재 배포 작업으로 실행하지 않습니다.

### 런타임 컴포넌트 일람 (Runtime Services)

| 런타임 모듈명 | 소스 디렉토리 | 중앙 컨테이너명 | 가동 포트 | 주요 역할 |
|---|---|---|---:|---|
| hololive-api | hololive-api | hololive-api | 30001/30006 | bot/admin/YouTube plane 통합 런타임: Kakao/Iris 웹훅 라우팅·챗 명령어 파싱(bot), 관리자 API 컨트롤 플레인(admin), YouTube observation 소비·정본 저장(YouTube) |
| alarm-worker | hololive-alarm-worker | hololive-alarm-worker | 30007 | 방송 정보 주기적 분석, 발송 대기열 소비 및 Iris outbound 호출 |
| youtube-collector | hololive-youtube-collector | hololive-youtube-collector-c | 30005/15/25/35 | 유튜브 수집 AP fleet: fetch/normalize/lease/Publish. PostgreSQL만 소비하며 Valkey를 요구하지 않습니다. |

### 공유 라이브러리 (Shared Libraries)

* **hololive-shared:** 홀로라이브 도메인 도큐먼트, 데이터 스키마 및 공통 비즈니스 로직 정의
* **shared-go:** 로그 처리, 트레이싱 등을 담당하는 범용 Go 공통 모듈 (Submodule 연계)

### 핵심 데이터 흐름 (Core Flow)

* 인바운드 메시지 처리: `Iris Core -> hololive-api (bot plane) -> Command/Service -> PostgreSQL & Valkey`
* 실시간 알림 발송: `alarm-worker -> PostgreSQL dispatch outbox (Valkey wakeup) -> alarm-worker egress -> Iris Core -> 카카오톡`
* 유튜브 신작 감지: `youtube-collector -> source_observations -> hololive-api YouTube plane -> alarm-worker`

Iris 연결은 `iris-client-go/v5 v5.0.0`을 사용하며 기동 epoch 인증을 지원하는 Iris가 필수입니다.
SDK는 보호 호출 전 같은 H3/TLS 대상의 공개 `GET /auth/epoch`를 조회·검증·캐시하고,
bootstrap 실패 시 보호 요청을 보내지 않으며 구형 인증으로 대체하지 않습니다.
`401`과 검증된 `AUTH_EPOCH_MISMATCH`가 함께 온 경우만 원래 deadline·본문·`ClientRequestID`를 유지해
논리 호출당 최대 한 번 epoch를 갱신하고 재서명합니다. 일반 401·5xx·전송 오류는 이 인증 복구 경로로
재시도하지 않으며 기존 owner 재시도·handoff·결과 불명·webhook v3·inbox 계약은 그대로입니다.
새 Iris와 소비자를 같은 배포 창에서 교체하고 롤백도 인증 호환 조합으로 수행해야 합니다.
정식 SDK tag 게시 뒤 공식 Go 도구로 checksum을 생성하기 전에는 운영 빌드·배포하지 않습니다.

---

## 모듈 경로 계약 (Module Path Contract)

`hololive/*`의 다섯 `go.mod`가 쓰는 `github.com/kapu/*` 모듈 경로는 go.work 전용 계약이다. 이 경로는 루트 `go.work`와 각 `go.mod`의 `replace ../…` 지시문으로만 해석되며 `go get`으로 받을 수 없다. 다섯 업무 모듈 분리는 루트 `go.work`의 `use` 목록이 소유하며, 이 목록에서 빠진 모듈은 아래 `go build`/`go test` 명령에서 바로 해석 실패한다. 외부 소비자가 없으므로 원격 경로와 맞추는 작업은 `hololive-shared`가 contracts·domain 수준으로 줄어든 뒤 모듈 수 축소와 함께 재검토한다(`DEC-20260902-hololive-module-layout-go-work-only`).

## 개발 및 검증 (Development & Test)

### 사전 요구 사양 (Prerequisites)

* Go 1.27 계열
* PostgreSQL (영속 저장소)
* Valkey (세션 및 큐)
* Podman (image build, 통합 검증, 중앙 Quadlet 운영)

### 1. 전체 소스 코드 컴파일 (Build)

```bash
go work sync
go build ../shared-go/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-api/... \
  ./hololive/hololive-alarm-worker/... \
  ./hololive/hololive-youtube-collector/...
```

### 2. 전체 단위 테스트 실행 (Test)

```bash
go test ../shared-go/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-api/... \
  ./hololive/hololive-alarm-worker/... \
  ./hololive/hololive-youtube-collector/...
```

* 아키텍처 가드레일 정적 검사:
  ```bash
  ./scripts/architecture/ci-boundary-gate.sh
  ```
* 로컬 통합 품질 게이트: `./scripts/ci/local-ci.sh`

`scripts/architecture/check-sql-ownership.sh`는 SQL 소유권과 함께 지정 경로의 시계 선택을 검사합니다.
Collector `joblease/queries/repository_lease_*`와 alarm의 member fence·sending·실패 전이는
`clock_timestamp()`를, collector 후보·target·현재 projection 및 API의 collection/live-check/time-finalization
조회는 `statement_timestamp()`를 사용합니다. 정확한 경로 규칙은 검사기의 `CLOCK_QUERY_POLICIES`가 소유합니다.
주석·문자열은 제외하며, 그 밖의 기록용 `NOW()`나 기존 migration을 일괄 금지하지 않습니다.
이 검사는 DB 경합 시험이 다루는 잠금 완료·시각 채취 순서나 호출자 트랜잭션을 증명하지 않습니다.

트리거 집계와 원본의 운영 정합 확인은 [API runbook](docs/current/runbooks/hololive-api.md#동기-집계-정합-확인)을 따릅니다.
dbtest와 진단은 `scripts/maintenance/queries/*_drift.sql`의 독립 오라클을 공유합니다.

`hololive-dbtest`와 로컬 CI가 생성하는 임시 PostgreSQL은 `/var/lib/postgresql`을 tmpfs에 두고
`fsync`·`synchronous_commit`·`full_page_writes`를 끕니다. 빌드 호스트의 디스크 쓰기를 줄이는
테스트 전용 설정이며, 컨테이너 중지 시 데이터가 사라지고 crash durability 검증에는 사용할 수 없습니다.
외부 `TEST_DATABASE_URL`과 운영 PostgreSQL 설정은 변경하지 않습니다.

DB 기준선은 epoch-3입니다. 기존 epoch-2 DB는 372 head의 233개 파일명·checksum 원장이 모두 일치할 때만 전환합니다.
부분 적용 백업은 이전 릴리스로 먼저 올려야 하며, 신규 migrator 실행 후의 롤백은 전환 전 백업의 별도 복원이 필요합니다.
[적용·복구 규약](hololive/hololive-api/scripts/migrations/CONVENTIONS.md)을 먼저 확인합니다.

```bash
go test ./hololive/hololive-dbtest/... \
  ./hololive/hololive-api/internal/migrationrunner/... \
  ./hololive/hololive-api/cmd/db-migrate/...
```

배포 스크립트(`./build-all.sh`) 기동 시, Docker 이미지 빌드 단계 진입 전에 `local-ci.sh` 품질 게이트가 자동으로 선행 수행됩니다. 해당 품질 검사(golangci-lint, NilAway, 경합 테스트 등) 중 하나라도 실패하면 빌드 프로세스가 강제 차단됩니다. 의존성 취약점 진단(`govulncheck`)과 배포 스크립트 테스트는 `scripts/ci/pre-push-gate.sh`가 push 시점에 실행합니다.

---

## 운영 배포 절차 (Deployment)

운영계 배포 및 롤아웃은 아래 배포 스크립트로 모듈별로 안전하게 진행됩니다.

11.0.0 worker/DB 전환은 구 프로필 호환 10.2.0의 실제 수용 뒤에만 진행합니다.
[단일 settings schema·pairing·DB 정책 순서](deploy/compose/README.md#worker-settings-cutover-1100)를
따르며 source 통합을 운영 수용으로 간주하지 않습니다. epoch-3 전환은 위 migration 규약을 따르며
구 manifest로 되돌리지 않습니다. Role limit은 네 collector의 max 7 수용 뒤
rollout 소유자만 활성화합니다.

```bash
# 전체 production image를 podman build (clean tree면 revision label을 검증)
./build-all.sh --no-bump --build-only

# 개별 image만 빌드
./build-all.sh --no-bump hololive-api
```

중앙 배포(image 전송·Quadlet 기동)는 [릴리즈 runbook](docs/current/runbooks/release.md)을 `hololive-bot-ops`로 라우팅하십시오.

원격 인프라 노드(`youtube-collector-b`) 작업은 [youtube-collector runbook](docs/current/runbooks/youtube-collector.md)을 `hololive-bot-ops`로 라우팅하십시오.

---

## 서비스 상태 진단 (Logs & Health Check)

* 로그 분석 표준(SSOT): 개발 및 문제 원인 추적 시 각 서비스의 표준 출력/표준 에러(stdout/stderr)를 1순위 데이터로 다룹니다. 중앙과 iris-seoul collector b는 journald(`journalctl -u <unit>`), host-native a/d도 journald입니다.
* 로컬 파일 로그: 디버깅 시 참고용 보조 미러 정보로만 취급합니다.

### 서비스별 상태 진단 엔드포인트

| 런타임 모듈 | 상태 검증 URI (Health Check) |
|---|---|
| hololive-api (bot) | `https://127.0.0.1:30001/health` via container `./bin/healthcheck` |
| hololive-api (admin) | `https://127.0.0.1:30006/health` via container `./bin/healthcheck` |
| alarm-worker | `https://127.0.0.1:30007/health` via container `./bin/healthcheck` |
| youtube-collector | `https://127.0.0.1:30025/ready` via container `./bin/healthcheck` (Main 노드) |

```bash
# 중앙 unit 상태 점검
sudo systemctl status hololive-api.service hololive-alarm-worker.service hololive-youtube-collector-c.service

# 실시간 로그
sudo journalctl -u <unit>.service -f

# 특정 시점 범위의 로그 필터링 쿼리
./scripts/logs/logs.sh query <service_name> --since 1h --limit 1000
```

---

## 연관 기술 문서 (Related Documentation)

* [**docs/README.md**](docs/README.md) - 전체 기술 문서 보관용 인덱스
* [**docs/current/PROJECT_MAP.md**](docs/current/PROJECT_MAP.md) - 현 시점의 전체 런타임 및 아키텍처 인벤토리 맵 (SSOT)
* [**docs/current/SERVICE_OWNERSHIP.md**](docs/current/SERVICE_OWNERSHIP.md) - 서비스 모듈별 관리 소유권 명세
* [**docs/current/CONTRACT_MAP.md**](docs/current/CONTRACT_MAP.md) - 각 컴포넌트 간 통신 규격 프로토콜 계약 지도
