# OpenAPI Pipeline: Go Backend → TypeScript Frontend

> **목표**: Go 백엔드의 API 스펙을 자동으로 추출하여 TypeScript 프론트엔드에서 타입 안전한 API 클라이언트를 자동 생성한다.

## 1. 개요

### 1.1 문제점
- Go 구조체와 TypeScript 인터페이스 간 **수동 동기화** 필요
- API 변경 시 프론트엔드 타입 업데이트 누락 가능성
- 런타임에서야 발견되는 타입 불일치

### 1.2 해결책: OpenAPI Generator Pipeline

```
┌─────────────────────────────────────────────────────────────────────┐
│                         Build Pipeline                               │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   Go Backend              OpenAPI Spec              TS Frontend      │
│   ┌───────────┐          ┌───────────┐           ┌───────────┐      │
│   │ Gin       │  swag    │ openapi.  │  openapi  │ api/      │      │
│   │ Handlers  │ ──────►  │ json      │ ──────►   │ ├─ types  │      │
│   │ + swag    │  init    │           │  generator│ └─ client │      │
│   │ comments  │          │           │           │           │      │
│   └───────────┘          └───────────┘           └───────────┘      │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 1.3 적용 대상

| 프로젝트 | 역할 | 우선순위 |
|---------|------|---------|
| `admin-dashboard` | 인프라 관리 API (Auth, Docker, Logs, Status) | **P0** |
| `hololive-kakao-bot-go` | 도메인 Admin API (`/admin/api/holo/*`) | P1 |
| `game-bot-go` | 게임 도메인 API (`/admin/api/twentyq/*`, `/admin/api/turtle/*`) | P2 |

---

## 2. 구현 계획

### Step 1: swag 도구 설치

```bash
# 전역 설치 (개발 환경)
go install github.com/swaggo/swag/cmd/swag@latest

# 프로젝트별 의존성 추가 (선택)
go get -u github.com/swaggo/gin-swagger
go get -u github.com/swaggo/files
```

### Step 2: API 문서 주석 추가

#### 2.1 main.go에 전역 메타데이터 추가

```go
// @title           Admin Backend API
// @version         1.0.0
// @description     Unified Admin Console Backend - Infrastructure Management APIs
// @termsOfService  https://admin.capu.blog/terms

// @contact.name   API Support
// @contact.email  admin@capu.blog

// @license.name  MIT
// @license.url   https://opensource.org/licenses/MIT

// @host      admin.capu.blog
// @BasePath  /admin/api

// @securityDefinitions.apikey  SessionCookie
// @in                          cookie
// @name                        session_id
// @description                 Session-based authentication via HTTP-only cookie

// @tag.name        auth
// @tag.description Authentication endpoints (login, logout, heartbeat)

// @tag.name        docker
// @tag.description Docker container management

// @tag.name        logs
// @tag.description System and container log access

func main() {
    // ...
}
```

#### 2.2 핸들러에 Swagger 주석 추가

```go
// handleLogin godoc
// @Summary      User login
// @Description  Authenticate with username and password
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        request  body      LoginRequest  true  "Login credentials"
// @Success      200      {object}  LoginResponse
// @Failure      401      {object}  ErrorResponse  "Invalid credentials"
// @Failure      429      {object}  ErrorResponse  "Too many attempts"
// @Router       /auth/login [post]
func (s *Server) handleLogin(c *gin.Context) {
    // ...
}

// handleDockerContainers godoc
// @Summary      List containers
// @Description  Get all managed Docker containers with status
// @Tags         docker
// @Accept       json
// @Produce      json
// @Security     SessionCookie
// @Success      200  {array}   ContainerInfo
// @Failure      401  {object}  ErrorResponse  "Unauthorized"
// @Failure      503  {object}  ErrorResponse  "Docker unavailable"
// @Router       /docker/containers [get]
func (s *Server) handleDockerContainers(c *gin.Context) {
    // ...
}
```

### Step 3: Makefile에 swagger 타겟 추가

```makefile
SWAG ?= swag

.PHONY: swagger
swagger:
	$(SWAG) init -g cmd/admin/main.go -o ./docs --parseDependency --parseInternal

.PHONY: swagger-fmt
swagger-fmt:
	$(SWAG) fmt -g cmd/admin/main.go
```

### Step 4: OpenAPI Generator로 TS 클라이언트 생성

#### 4.1 프론트엔드 package.json 스크립트 추가

```json
{
  "scripts": {
    "generate:api": "openapi-generator-cli generate -i ../backend/docs/swagger.json -g typescript-fetch -o src/api/generated --additional-properties=supportsES6=true,typescriptThreePlus=true"
  },
  "devDependencies": {
    "@openapitools/openapi-generator-cli": "^2.7.0"
  }
}
```

#### 4.2 생성되는 파일 구조

```
frontend/src/api/
├── generated/           # 자동 생성 (Git ignore)
│   ├── apis/
│   │   ├── AuthApi.ts
│   │   ├── DockerApi.ts
│   │   └── LogsApi.ts
│   ├── models/
│   │   ├── LoginRequest.ts
│   │   ├── LoginResponse.ts
│   │   ├── ContainerInfo.ts
│   │   └── ...
│   └── index.ts
├── client.ts            # 커스텀 설정 래퍼
└── index.ts             # Re-export
```

### Step 5: CI/CD 통합

```yaml
# .github/workflows/openapi.yml
name: OpenAPI Sync

on:
  push:
    paths:
      - 'admin-dashboard/backend/internal/server/**'

jobs:
  generate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Install swag
        run: go install github.com/swaggo/swag/cmd/swag@latest
        
      - name: Generate OpenAPI spec
        run: |
          cd admin-dashboard/backend
          swag init -g cmd/admin/main.go -o ./docs
          
      - name: Generate TypeScript client
        run: |
          cd admin-dashboard/frontend
          npm run generate:api
          
      - name: Create PR
        uses: peter-evans/create-pull-request@v5
        with:
          title: "chore: sync OpenAPI types"
          branch: openapi-sync
```

---

## 3. DTO/Response 타입 정의 표준

### 3.1 요청/응답 타입 분리

```go
// internal/server/types.go

// LoginRequest: 로그인 요청
type LoginRequest struct {
    Username string `json:"username" binding:"required" example:"admin"`
    Password string `json:"password" binding:"required" example:"password123"`
}

// LoginResponse: 로그인 응답
type LoginResponse struct {
    Success bool   `json:"success" example:"true"`
    Message string `json:"message" example:"Login successful"`
}

// ErrorResponse: 공통 에러 응답
type ErrorResponse struct {
    Error   string `json:"error" example:"Unauthorized"`
    Code    int    `json:"code" example:"401"`
    Details string `json:"details,omitempty" example:"Invalid session"`
}
```

### 3.2 기존 내부 타입 재활용

기존 `docker.ContainerInfo` 등은 swag이 자동으로 인식합니다.
단, `json` 태그와 `example` 태그가 필요합니다.

---

## 4. 예상 이점

| 항목 | Before | After |
|------|--------|-------|
| 타입 동기화 | 수동 (에러 prone) | **자동** |
| API 문서 | 없음 | Swagger UI 제공 |
| 프론트 개발 시간 | 타입 정의 포함 | **타입 정의 자동화** |
| 런타임 에러 | 타입 불일치 발견 어려움 | **컴파일 타임 검출** |
| 코드 리뷰 | 타입 변경 확인 어려움 | OpenAPI diff로 명확화 |

---

## 5. 롤아웃 계획

### Phase 1: admin-dashboard ✅ 완료 (2026-01-02)

| 작업 | 상태 |
|------|------|
| 문서 작성 | ✅ |
| swag 전역 주석 추가 (main.go) | ✅ |
| swag 핸들러 주석 추가 (19개 엔드포인트) | ✅ |
| DTO 타입 정의 (`internal/server/types.go`) | ✅ |
| Makefile `swagger` 타겟 추가 | ✅ |
| 프론트엔드 generator 설정 | ✅ |
| 라우터 도메인별 분리 | ✅ |
| 린트 오류 해결 (8개) | ✅ |
| OpenAPI 스펙 생성 (`docs/swagger.json`) | ✅ |

**생성된 파일:**
- `backend/docs/swagger.json` (39KB)
- `backend/docs/swagger.yaml`
- `backend/docs/docs.go`

**라우터 분리 구조:**
```go
func (s *Server) setupRoutes() {
    s.setupAuthRoutes(api)           // /login, /logout, /heartbeat
    s.setupDockerRoutes(authenticated) // /docker/*
    s.setupLogsRoutes(authenticated)   // /logs/*
    s.setupStatusRoutes(authenticated) // /status, /ws/*
    s.setupProxyRoutes(authenticated)  // /holo/*, /twentyq/*, /turtle/*
    s.setupHealthRoute()               // /health
    s.setupStaticRoutes()              // /assets/*, SPA fallback
}
```

### Phase 2: hololive-kakao-bot-go (다음)

1. ⬜ `/admin/api/holo/*` 엔드포인트에 swag 주석 추가
2. ⬜ 별도 `openapi-holo.json` 생성 또는 통합 스펙에 병합

### Phase 3: game-bot-go ✅ 전체 완료 (2026-01-02)

| 작업 | 상태 |
|------|------|
| TwentyQ Admin API 기본 (`/admin/stats`, `/sessions`, `/games`, `/leaderboard`) | ✅ |
| TwentyQ CMS API (`/admin/synonyms`, `/admin/games/{id}/audit`, `/admin/games/{id}/refund`) | ✅ |
| TurtleSoup Admin API 기본 (`/admin/stats`, `/sessions`, `/cleanup`, `/inject`) | ✅ |
| Valkey SCAN 구현 (활성 세션 조회) | ✅ |
| admin-dashboard 프록시 경로 설정 | ✅ |
| API 문서 작성 (`game-bot-go/docs/api/admin_api.md`) | ✅ |
| Swag 주석 추가 | ⬜ (향후) |

---

## 6. 사용법

### 백엔드: OpenAPI 스펙 생성

```bash
cd admin-dashboard/backend
make swagger
# → docs/swagger.json, docs/swagger.yaml 생성
```

### 프론트엔드: TypeScript 클라이언트 생성

```bash
cd admin-dashboard/frontend
npm install
npm run generate:api
# → src/api/generated/ 디렉토리에 타입 및 클라이언트 생성
```

---

## 7. 참고 자료

- [swaggo/swag GitHub](https://github.com/swaggo/swag)
- [OpenAPI Generator](https://openapi-generator.tech/)
- [Swagger UI](https://swagger.io/tools/swagger-ui/)
