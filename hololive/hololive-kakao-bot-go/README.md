# Hololive KakaoTalk Bot (Go)

> 홀로라이브 VTuber 스케줄, 정보 검색 및 알림을 제공하는 고성능 카카오톡 봇 (Go 버전)

카카오톡을 통해 홀로라이브 소속 VTuber들의 실시간 방송 현황, 예정된 스케줄, 공식 프로필 정보를 빠르고 편리하게 제공합니다. Go 1.26.3의 최신 기능과 Valkey 기반의 다층 캐싱 시스템, GORM 기반의 안정적인 데이터 관리를 통해 높은 성능과 확장성을 보장합니다.

## ✨ 주요 기능

-   **실시간 방송 조회 (`!라이브`)**: Holodex API와 연동하여 현재 방송 중인 멤버 확인
-   **스케줄 정보 (`!예정`)**: 향후 24시간 내의 방송 예정 스케줄 조회
-   **멤버 정보 & 검색**: 공식 프로필 데이터 기반 상세 정보 제공 (한국어 번역 포함)
-   **스마트 알림 시스템**:
    -   방송 시작 전 알림 (5분, 15분, 30분 전 등 설정 가능)
    -   개인화된 멤버별 알림 구독/해제 (`!알람`)
-   **관리 API**: `/api/holo/*`, `/api/auth/*` 엔드포인트로 운영/설정 제어
-   **동적 ACL (접근 제어)**: 카카오톡 채팅방 별 접근 허용/차단 동적 관리
-   **멀티 플랫폼 방송 감지**:
    -   **YouTube**: Holodex API 연동, OAuth 폴링, 구독자 통계
    -   **Chzzk**: 네이버 치지직 라이브 감지 + YouTube 하이브리드
    -   **Twitch**: 트위치 라이브 감지, 유저/스트림 ID 기반
-   **LLM 이벤트 요약**: Cliproxy+Exa 기반 주요 이벤트 자동 요약 (`service/majorevent/`)
-   **강력한 성능**:
    -   **HTTP/2 (H2C)**: 멀티플렉싱 지원으로 통신 효율 극대화
    -   **Valkey Caching**: API 호출 비용 절감 및 응답 속도 최적화
    -   **ValkeyMQ**: 안정적인 메시지 큐 기반 비동기 처리
    -   **Circuit Breaker**: 외부 API 장애 시 자동 차단 및 복구
-   **멀티 그룹 지원**: 홀로라이브 외 다른 VTuber 그룹 지원
    -   **니지산지 (Nijisanji)**: 니지산지 소속 VTuber 방송 조회 및 알림
    -   **VSPO**: VSPO 소속 VTuber 방송 조회 및 알림
    -   **개인세 (Indie)**: 등록된 개인 VTuber 방송 조회 및 알림

## 🛠 기술 스택

이 프로젝트는 최신 Go 생태계와 안정적인 오픈소스를 활용하여 구축되었습니다.

-   **Language**: [Go](https://go.dev/) 1.26.3
-   **Web Framework**: [Gin](https://github.com/gin-gonic/gin) 1.11.0 (High-performance HTTP web framework)
    -   **Protocol**: HTTP/2 Cleartext (H2C) via `golang.org/x/net/http2/h2c`
-   **Database**: PostgreSQL 16+
    -   **ORM**: [GORM](https://gorm.io/) (PostgreSQL Driver)
-   **Cache & MQ**: [Valkey](https://valkey.io/) (Open Source Redis Monitor)
    -   **Client**: `valkey-io/valkey-go`
-   **Logging**: `log/slog` (Go Standard Library)
    -   **Handler**: `lmittmann/tint` (stdout structured logging, local file logging optional)
-   **Concurrency**: `sourcegraph/conc` (Structured concurrency)
-   **Infrastructure**:
    -   **Messenger**: Iris (카카오톡 연동 미들웨어)
    -   **Monitoring**: Deunhealth (컨테이너 상태 모니터링 및 자동 복구)
    -   **Deployment**: Docker & Docker Compose

## 📂 프로젝트 구조

현재 `hololive-kakao-bot-go`는 bot ingress 런타임을 제공합니다.
runtime split 기준으로 control plane, alarm worker, scheduler, ingestion 런타임은 별도 module에서 운영합니다.

```
hololive/
├── hololive-kakao-bot-go/       # bot
│   ├── cmd/bot
│   ├── cmd/tools
│   └── internal/{app,bot,command,server,service/{acl,activity,auth,system}}
├── hololive-admin-api/          # admin-api
├── hololive-alarm-worker/       # alarm-worker
├── hololive-llm-sched/          # llm-scheduler
├── hololive-stream-ingester/    # stream-ingester
├── hololive-shared/             # 공통 domain/service/providers/server
└── shared-go/                   # 공통 유틸리티
```

### 런타임 분리 상태

- `cmd/bot` → webhook ingress + command routing
- `hololive-admin-api/cmd/admin-api` → `/api/holo/*`, `/api/auth/*`, `/oauth/callback`, `/internal/alarm/*`
- `hololive-alarm-worker/cmd/alarm-worker` → alarm scheduler / checker / queue publisher/consumer/proactive egress
- `cmd/llm-scheduler` → `hololive-llm-sched/cmd/llm-scheduler`
- `cmd/stream-ingester` → `hololive-stream-ingester/cmd/stream-ingester`
- 서비스별 Dockerfile → 각 모듈 루트 `Dockerfile`

## 📂 (레거시) 단일 모듈 구조 스냅샷

아래는 분리 이전(참고용) 구조입니다.

```
hololive-kakao-bot-go/
├── cmd/
│   ├── bot/                      # Main Bot Entrypoint
│   └── tools/                    # 데이터 관리 및 유틸리티 도구
├── internal/
│   ├── adapter/                  # 메시지 포맷팅 및 외부 인터페이스 어댑터
│   ├── app/                      # 애플리케이션 라이프사이클 및 DI (Manual Injection)
│   ├── assets/                   # 임베디드 정적 리소스
│   ├── bot/                      # 봇 핵심 로직 및 오케스트레이션
│   ├── command/                  # 명령어 핸들러 (!라이브, !예정 등)
│   ├── config/                   # 환경 설정 관리
│   ├── constants/                # 공통 상수 정의
│   ├── domain/                   # 도메인 모델 정의 (GORM Models)
│   ├── health/                   # 헬스체크
│   ├── iris/                     # Iris 메시징 (webhook, h2c 클라이언트)
│   ├── platform/                 # 인프라 스트럭처 (DB, Cache 연결 등)
│   ├── repository/               # 데이터 접근 계층
│   ├── server/                   # HTTP/H2C 서버 및 미들웨어
│   ├── service/                  # 비즈니스 로직 (YouTube, Chzzk, Twitch, Alarm 등)
│   └── util/                     # 유틸리티 함수
├── data/                         # 임베디드 정적 데이터 (번역된 프로필 등)
├── scripts/                      # 로컬 실행/DB 마이그레이션 스크립트
└── Dockerfile                    # 프로덕션 배포용 Docker 설정
```

## 🚀 시작하기

### 사전 요구사항

-   Go 1.26.3
-   Valkey (또는 Redis) 서버
-   PostgreSQL 데이터베이스
-   Iris 메신저 서버 (카카오톡 연동용)
-   Holodex API Key

### 로컬 실행 (개발용)

1.  **환경 변수 설정**:
    ```bash
    cp .env.example .env
    # .env 파일을 열어 필요한 설정(API Key, DB 정보 등)을 입력하세요.
    ```

2.  **데이터베이스 초기화**:
    GORM Auto Migration을 통해 테이블이 자동으로 생성됩니다.

3.  **빌드 및 실행**:
    ```bash
    # 의존성 설치
    go mod download

    # 실행
    go run ./cmd/bot
    
    # 또는 로컬 보조 스크립트 사용
    ./scripts/bot.sh start
    ```

### Docker Compose 배포 (프로덕션)

이 저장소는 루트 `docker-compose.prod.yml` 기준으로 통합 배포합니다.

```bash
./build-all.sh --no-bump
./scripts/deploy/compose-redeploy-service.sh hololive-bot
./scripts/deploy/compose-redeploy-service.sh hololive-admin-api
./scripts/deploy/compose-redeploy-service.sh hololive-alarm-worker
```

전체 스택/상세 절차는 `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`를 참고하세요.

runtime split 이후 운영 책임은 다음과 같습니다.

- `hololive-bot` → `/webhook/iris`, command routing, ingress
- `hololive-admin-api` → `/api/holo/*`, `/api/auth/*`, `/oauth/callback`, `/internal/alarm/*`
- `hololive-alarm-worker` → alarm scheduler / checker / queue publisher

롤백 시에도 동일하게 각 runtime을 개별 재기동해야 하며, admin/control plane 문제는 `hololive-bot`이 아니라 `hololive-admin-api`를 기준으로 되돌립니다.

로컬 단일 프로세스 보조 스크립트는 `scripts/README.md`를 참고하세요.

## ⚙️ 환경 변수 설정 (`.env`)

주요 설정 항목은 다음과 같습니다.

| 카테고리 | 변수명 | 설명 | 기본값 |
| :--- | :--- | :--- | :--- |
| **서버** | `SERVER_PORT` | 봇 웹 서버 포트 | `30001` |
| **Holodex** | `HOLODEX_API_KEY` | Holodex API 키 | **필수** |
| **YouTube** | `YOUTUBE_API_KEY` | YouTube Data API 키 (구독자 수 조회용) | - |
| **Kakao** | `KAKAO_ROOMS` | 봇이 응답할 카카오톡 방 이름 목록 (쉼표 구분) | `홀로라이브 알림방` |
| | `KAKAO_ACL_ENABLED` | ACL(접근 제어) 활성화 여부 | `true` |
| **Iris** | `IRIS_BASE_URL` | Iris 메신저 서버 주소 | **필수** |
| | `IRIS_WEBHOOK_TOKEN` | Iris -> bot inbound webhook 인증 토큰 | **필수** |
| | `IRIS_BOT_TOKEN` | bot -> Iris reply/send 인증 토큰 | **필수** |
| **DB** | `POSTGRES_HOST`, `_PORT`, ... | PostgreSQL 연결 정보 | `localhost`, `5432` |
| **Cache** | `CACHE_HOST`, `_PORT` | Valkey(Redis) 캐시 서버 정보 | `localhost`, `6379` |
| **MQ** | `MQ_HOST`, `_PORT` | ValkeyMQ 서버 정보 | `localhost`, `1833` |
| **Logging** | `LOG_LEVEL` | 로그 레벨 (`debug`, `info`, `warn`, `error`) | `info` |

> 참고: 관리자 콘솔(Auth/Docker/Logs/Traces)은 `admin-dashboard`로 분리되었고, 운영 API는 `hololive-bot`이 아니라 `hololive-admin-api`가 제공합니다.

## 🌐 지원 그룹

현재 지원되는 VTuber 그룹 및 소속사입니다.

| 그룹 | 설명 | 동기화 방식 |
|------|------|-------------|
| **Hololive** | 홀로라이브 프로덕션 소속 VTuber | Holodex API 자동 동기화 |
| **Nijisanji** | 니지산지 소속 VTuber | Holodex API 자동 동기화 |
| **VSPO** | VSPO 소속 VTuber | Holodex API 자동 동기화 |
| **Indie** | 등록된 개인 VTuber | 수동 등록 |

### 그룹 태그 표시

라이브/예정 목록에서 홀로라이브 외 그룹은 태그로 구분됩니다:

```
🔴 라이브 중인 방송 (3개)

• 사쿠라 미코 - 마인크래프트
• [니지산지] 쿠제 혼지 - 잡담
• [VSPO] 아카사키 치호 - 발로란트
• [개인세] 유우키 사쿠나 - 노래방
```

### 동명이인 처리

다른 그룹에 동일한 이름의 멤버가 있을 경우, `!알람` 명령어에서 선택 리스트가 표시됩니다:

```
동일한 이름의 멤버가 여러 명 있습니다:

1. 미코 (Hololive)
2. 미코 (Nijisanji)

정확한 멤버를 지정하려면 다음과 같이 입력해주세요:
!알람 추가 미코 (Hololive)
```

## 🕹 명령어 목록

봇이 있는 채팅방에서 아래 명령어를 사용할 수 있습니다. (`!` 접두사 기준)

-   **방송 확인**
    -   `!라이브`: 현재 방송 중인 모든 멤버 목록
    -   `!라이브 [멤버명]`: 특정 멤버의 생방송 여부 확인
    -   `!예정`: 향후 24시간 내 예정된 방송 목록
    -   `!멤버 [이름]`: 해당 멤버의 주간 스케줄 확인

-   **정보 조회**
    -   `!정보 [멤버명]`: 멤버 프로필 및 상세 정보 (예: `!정보 미코`)
    -   `!구독자순위`: 멤버들의 구독자 수 및 최근 급상승 순위 (TOP 10)

-   **알림 관리**
    -   `!알람 추가 [멤버명]`: 해당 멤버의 방송 알림 받기
    -   `!알람 추가 [멤버명] (그룹)`: 동명이인이 있을 경우 그룹 지정 (예: `!알람 추가 미코 (Nijisanji)`)
    -   `!알람 제거 [멤버명]`: 해당 멤버의 알림 끄기
    -   `!알람 목록`: 현재 구독 중인 알림 목록 확인
    -   `!알람 초기화`: 모든 알림 설정 초기화

-   **기타**
    -   `!도움말`: 명령어 도움말 확인

## 🛡 관리 및 모니터링

-   **Health Check**: `/health` 엔드포인트를 통해 봇의 상태를 확인할 수 있습니다.
-   **Deunhealth**: 컨테이너가 멈추거나 헬스 체크에 실패하면 `deunhealth`가 자동으로 이를 감지하고 재시작하여 가용성을 유지합니다.
-   **Graceful Shutdown**: 종료 시그널(SIGTERM) 수신 시 진행 중인 작업을 안전하게 마무리하고 종료합니다.

## 📋 마이그레이션

### v2.1.0 - 멀티 그룹 지원 (2026-01)

멀티 그룹 지원을 위해 데이터베이스 마이그레이션이 필요합니다.

```bash
# 마이그레이션 실행
psql -d <database> -f scripts/migrations/016-add-multi-group-support.sql
```

**변경 사항:**
- `members` 테이블에 `org`, `suborg`, `sync_source` 컬럼 추가
- 기존 데이터는 `org='Hololive'`, `sync_source='holodex'`로 자동 백필
- 개인세 VTuber 시드 데이터 추가 (유우키 사쿠나, 사메코 사바)

## 📝 라이선스

MIT License. See [LICENSE](LICENSE) for details.

### Third-Party API Attribution

#### Holodex API

이 프로젝트는 [Holodex API](https://holodex.net)를 사용하여 VTuber 스트림 정보를 제공합니다.

-   **API Provider**: [Holodex](https://holodex.net)
-   **Terms of Service**: [https://holodex.net/api/terms](https://holodex.net/api/terms)
-   **Disclaimer**: THE HOLODEX API IS PROVIDED "AS IS" WITHOUT WARRANTY OF ANY KIND.

Holodex API Terms of Service Section 6 (Attribution)에 따라 본 프로젝트는 Holodex를 API 제공자로 명시하며, 소스 코드 내에 라이선스 및 면책 조항에 대한 참조를 포함합니다.
