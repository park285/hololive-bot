# Admin Dashboard

관리 대시보드 - 백엔드(Go)와 프론트엔드(React)를 포함합니다.

## 구조

```
admin-dashboard/
├── backend/          # Go 백엔드
│   ├── cmd/admin/    # 엔트리포인트
│   ├── internal/     # 내부 패키지
│   │   ├── auth/     # 인증 및 세션
│   │   ├── bootstrap/# 앱 초기화
│   │   ├── config/   # 설정
│   │   ├── docker/   # Docker 관리
│   │   ├── logs/     # 시스템 로그
│   │   ├── middleware/# HTTP 미들웨어
│   │   ├── proxy/    # 봇 프록시
│   │   ├── server/   # HTTP 서버
│   │   ├── ssr/      # 서버사이드 렌더링
│   │   ├── static/   # 정적 파일 서빙
│   │   ├── status/   # 서비스 상태 관리
│   │   └── metrics/  # 인증/서버 메트릭 유틸
│   ├── Dockerfile
│   └── go.mod
└── frontend/         # React 프론트엔드
    ├── src/
    ├── package.json
    └── vite.config.ts
```

## 백엔드

### 빌드

```bash
cd backend
go build -tags=go_json -o admin ./cmd/admin
```

### 환경 변수

| 변수 | 설명 | 기본값 |
|------|------|--------|
| `PORT` | HTTP 포트 | `30190` |
| `VALKEY_URL` | Valkey 주소 | `valkey-cache:6379` |
| `DOCKER_HOST` | Docker 데몬 | `tcp://docker-proxy:2375` |
| `HOLO_BOT_URL` | hololive Admin API 업스트림 | `http://hololive-kakao-bot-go:30001` |
| `LOG_DIR` | 로그 디렉토리 | `/app/logs` |
| `ADMIN_USER` | 관리자 ID | `admin` |
| `ADMIN_PASS_HASH` | 비밀번호 bcrypt 해시 | - |
| `SESSION_SECRET` | 세션 서명 키 | - |
| `OTEL_ENABLED` | OpenTelemetry 활성화 | `false` |
| `OTEL_SERVICE_NAME` | OpenTelemetry 서비스명 | `admin-dashboard` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP 엔드포인트 | - |
| `OTEL_EXPORTER_OTLP_INSECURE` | OTLP insecure 사용 여부 | `true` |
| `OTEL_SAMPLE_RATE` | 샘플링 비율 (0.0~1.0) | `1.0` |

### 보안 설정 (신규)

| 변수 | 설명 | 기본값 |
|------|------|--------|
| `ALLOWED_ORIGINS` | CORS/WS 허용 Origin (comma-separated) | fallback 사용 |
| `ALLOW_LOCALHOST_IN_PROD` | 프로덕션에서 localhost Origin 허용 | `false` |
| `CSRF_MODE` | CSRF 보호 모드 (`enforce`/`monitor`/`off`) | `enforce` |
| `WS_ORIGIN_MODE` | WebSocket Origin 검증 모드 | `enforce` |
| `STREAM_LIMIT_MODE` | 스트림 제한 모드 | `enforce` |
| `GLOBAL_STREAM_LIMIT` | 전역 동시 스트림 제한 | `10` |
| `PER_SESSION_STREAM_LIMIT` | 세션당 동시 스트림 제한 | `2` |

**3상태 모드 설명:**
- `enforce`: 위반 시 403/429 반환 (기본값)
- `monitor`: 위반 시 로그만 남기고 허용 (관측용)
- `off`: 검증 건너뛰기 (개발용)

## 프론트엔드

### 개발 서버

```bash
cd frontend
npm install
npm run dev
```

### 빌드

```bash
cd frontend
npm run build
```

## Docker

```bash
# 현재 monorepo 루트에서
docker build -t admin-dashboard -f admin-dashboard/Dockerfile .
```

## API 경로

### 공통 (admin-dashboard)
- `POST /admin/api/auth/login` - 로그인
- `POST /admin/api/auth/logout` - 로그아웃
- `POST /admin/api/auth/heartbeat` - 세션 갱신
- `GET /admin/api/docker/*` - Docker 관리
- `GET /admin/api/logs/*` - 시스템 로그

### 도메인별 (프록시)
- `/admin/api/holo/*` → hololive Admin API (`hololive-kakao-bot-go`)
