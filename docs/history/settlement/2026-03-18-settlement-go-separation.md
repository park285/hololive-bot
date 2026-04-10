# Settlement-Go 독립 레포 분리 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** settlement-go를 hololive-bot 모노레포에서 `~/gemini/settlement-go/` 독립 레포로 완전 분리한다.

**Architecture:** 코드를 새 레포에 복사한 뒤, shared 의존성(iris, logging, cache, server)을 settlement 전용 축소 구현으로 교체한다. DB는 같은 PostgreSQL 인스턴스에 별도 `settlement` DB를 생성하고, 기존 데이터를 이관한다. Docker 배포는 자체 compose로 독립시키되 hololive-net external 네트워크로 인프라를 공유한다.

**Tech Stack:** Go 1.26, pgx/v5, valkey-go, lumberjack, tint, golang.org/x/net (h2c), PostgreSQL 17, Docker Compose

**Spec:** `docs/superpowers/specs/2026-03-18-settlement-go-separation-design.md`

---

## File Map

### 새 레포에 생성 (`~/gemini/settlement-go/`)

| 파일 | 책임 |
|------|------|
| `cmd/settlement/main.go` | 엔트리포인트, config, 서버 부트스트랩 |
| `cmd/settlement/handler.go` | webhook 핸들러, 커맨드 파싱 |
| `cmd/settlement/formatter.go` | 메시지 포맷터 |
| `cmd/settlement/main_test.go` | config/logger 테스트 |
| `cmd/settlement/server_test.go` | h2c 서버 테스트 |
| `internal/settlement/types.go` | 도메인 모델 (Member, Cycle, PaymentStatus) |
| `internal/settlement/repository.go` | PostgreSQL 저장소 |
| `internal/settlement/scheduler.go` | 정산 알람 스케줄러 |
| `internal/iris/client.go` | 축소 Iris h2c 클라이언트 (SendMessage만) |
| `internal/logging/logging.go` | 파일 로깅 (tint + lumberjack) |
| `internal/logging/sanitize.go` | 민감정보 마스킹 핸들러 |
| `internal/server/h2c.go` | WrapH2C 함수 |
| `internal/cache/cache.go` | 축소 Valkey 클라이언트 (Exists/Set/Close) |
| `scripts/migrations/001_init.sql` | DDL (테이블 3개 + 인덱스) |
| `scripts/seed.sql` | 개발환경 seed 데이터 |
| `scripts/migrate_data.sh` | 데이터 이관 스크립트 |
| `Dockerfile` | 단일 모듈 빌드 |
| `docker-compose.prod.yml` | settlement-bot + db-migrate |
| `.env.example` | 환경변수 예제 |
| `.gitignore` | 빌드 바이너리, .env 등 |
| `go.mod` | 독립 모듈 (replace 없음) |

### hololive-bot에서 변경

| 파일 | 변경 |
|------|------|
| `docker-compose.prod.yml` | settlement-bot 서비스 블록 삭제 |
| `hololive/settlement-go/` | 디렉토리 전체 삭제 |

---

## Task 1: 레포 초기화 + 기본 구조

**Files:**
- Create: `~/gemini/settlement-go/go.mod`
- Create: `~/gemini/settlement-go/.gitignore`
- Create: `~/gemini/settlement-go/.env.example`

- [ ] **Step 1: 디렉토리 생성 + git init**

```bash
mkdir -p ~/gemini/settlement-go
cd ~/gemini/settlement-go
git init
```

- [ ] **Step 2: .gitignore 작성**

```
# 빌드 바이너리
/settlement
/cmd/settlement/settlement
*.exe

# 환경변수
.env
.env.local
.env.*.local

# IDE
.idea/
.vscode/
*.swp
*.swo

# OS
.DS_Store
Thumbs.db

# 로그
logs/
*.log

# Go
/vendor/
```

- [ ] **Step 3: go.mod 초기화**

```bash
cd ~/gemini/settlement-go
go mod init github.com/kapu/settlement-go
```

- [ ] **Step 4: .env.example 작성**

```env
# Settlement Bot
SETTLEMENT_PORT=30002
SETTLEMENT_ROOM_ID=
SETTLEMENT_ALLOW_ROOMS=

# PostgreSQL
POSTGRES_HOST=localhost
POSTGRES_PORT=5433
POSTGRES_DB=settlement
POSTGRES_USER=settlement_runtime
POSTGRES_PASSWORD=
POSTGRES_SSLMODE=require

# Iris
IRIS_BASE_URL=http://localhost:3000
IRIS_BOT_TOKEN=

# Valkey Cache
CACHE_HOST=localhost
CACHE_PORT=6379
CACHE_SOCKET_PATH=

# Logging
LOG_DIR=
LOG_LEVEL=info
LOG_MAX_SIZE_MB=100
LOG_MAX_BACKUPS=5
LOG_MAX_AGE_DAYS=30
LOG_COMPRESS=true
```

- [ ] **Step 5: 커밋**

```bash
cd ~/gemini/settlement-go
git add .gitignore go.mod .env.example
git commit -m "feat: init settlement-go repo"
```

---

## Task 2: internal/server — WrapH2C

**Files:**
- Create: `~/gemini/settlement-go/internal/server/h2c.go`

- [ ] **Step 1: h2c.go 작성**

```go
package server

import (
	"net/http"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// WrapH2C: HTTP/2 Cleartext 지원을 위해 핸들러를 래핑한다.
func WrapH2C(handler http.Handler) http.Handler {
	return h2c.NewHandler(handler, &http2.Server{})
}
```

- [ ] **Step 2: 의존성 추가**

```bash
cd ~/gemini/settlement-go
go get golang.org/x/net
```

- [ ] **Step 3: 빌드 확인**

```bash
cd ~/gemini/settlement-go
go build ./internal/server/
```
Expected: 성공 (에러 없음)

- [ ] **Step 4: 커밋**

```bash
cd ~/gemini/settlement-go
git add internal/server/ go.mod go.sum
git commit -m "feat: add h2c server wrapper"
```

---

## Task 3: internal/logging — 파일 로깅 + 민감정보 마스킹

**Files:**
- Create: `~/gemini/settlement-go/internal/logging/logging.go`
- Create: `~/gemini/settlement-go/internal/logging/sanitize.go`

- [ ] **Step 1: sanitize.go 작성**

hololive-shared/internal/logging/sanitize.go를 복사. 패키지명은 `logging` 유지. import 변경 없음 (stdlib만 사용).

소스: `/home/kapu/gemini/hololive-bot/hololive/hololive-shared/internal/logging/sanitize.go`

변경 사항: 라이선스 헤더 유지, 내용 동일.

- [ ] **Step 2: logging.go 작성**

hololive-shared/internal/logging/logging.go를 복사. 패키지명 `logging` 유지.

소스: `/home/kapu/gemini/hololive-bot/hololive/hololive-shared/internal/logging/logging.go`

변경 사항: 라이선스 헤더 유지, 내용 동일. 이 파일은 이미 독립적 (external import: `tint`, `go-isatty`, `lumberjack`만).

- [ ] **Step 3: 의존성 추가**

```bash
cd ~/gemini/settlement-go
go get github.com/lmittmann/tint
go get github.com/mattn/go-isatty
go get gopkg.in/natefinch/lumberjack.v2
```

- [ ] **Step 4: 빌드 확인**

```bash
cd ~/gemini/settlement-go
go build ./internal/logging/
```
Expected: 성공

- [ ] **Step 5: 커밋**

```bash
cd ~/gemini/settlement-go
git add internal/logging/ go.mod go.sum
git commit -m "feat: add file logging with sanitization"
```

---

## Task 4: internal/cache — 축소 Valkey 클라이언트

**Files:**
- Create: `~/gemini/settlement-go/internal/cache/cache.go`

- [ ] **Step 1: cache.go 작성**

축소 interface + valkey-go 직접 구현. settlement-go가 사용하는 `Exists`, `Set`, `Close`만 구현.

```go
package cache

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/valkey-io/valkey-go"
)

// Client: settlement 전용 축소 캐시 인터페이스.
type Client interface {
	Exists(ctx context.Context, key string) (bool, error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Close() error
}

// Config: Valkey 연결 설정.
type Config struct {
	Host       string
	Port       int
	SocketPath string
	Password   string
}

type service struct {
	client valkey.Client
	logger *slog.Logger
}

// NewClient: Valkey 클라이언트를 생성합니다.
func NewClient(ctx context.Context, cfg Config, logger *slog.Logger) (Client, error) {
	var addr string
	if cfg.SocketPath != "" {
		addr = cfg.SocketPath
	} else {
		addr = fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	}

	opts := valkey.ClientOption{
		InitAddress:  []string{addr},
		Password:     cfg.Password,
		DisableCache: true,
	}

	// UDS 모드
	if cfg.SocketPath != "" {
		socketPath := cfg.SocketPath
		opts.DialCtxFn = func(ctx context.Context, _ string, _ *net.Dialer, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			d.Timeout = 5 * time.Second
			return d.DialContext(ctx, "unix", socketPath)
		}
	}

	client, err := valkey.NewClient(opts)
	if err != nil {
		return nil, fmt.Errorf("valkey 연결 실패 (%s): %w", addr, err)
	}

	// 연결 확인
	if pingErr := client.Do(ctx, client.B().Ping().Build()).Error(); pingErr != nil {
		client.Close()
		return nil, fmt.Errorf("valkey ping 실패: %w", pingErr)
	}

	if logger != nil {
		logger.Info("valkey_connected", slog.String("addr", addr))
	}

	return &service{client: client, logger: logger}, nil
}

func (s *service) Exists(ctx context.Context, key string) (bool, error) {
	result, err := s.client.Do(ctx, s.client.B().Exists().Key(key).Build()).AsInt64()
	if err != nil {
		return false, fmt.Errorf("exists %q: %w", key, err)
	}
	return result > 0, nil
}

func (s *service) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal value for %q: %w", key, err)
	}

	cmd := s.client.B().Set().Key(key).Value(string(data))
	if ttl > 0 {
		cmd.Ex(ttl)
	}

	if err := s.client.Do(ctx, cmd.Build()).Error(); err != nil {
		return fmt.Errorf("set %q: %w", key, err)
	}
	return nil
}

func (s *service) Close() error {
	s.client.Close()
	return nil
}
```

- [ ] **Step 2: 의존성 추가**

```bash
cd ~/gemini/settlement-go
go get github.com/valkey-io/valkey-go
```

- [ ] **Step 3: 빌드 확인**

```bash
cd ~/gemini/settlement-go
go build ./internal/cache/
```
Expected: 성공

- [ ] **Step 4: 커밋**

```bash
cd ~/gemini/settlement-go
git add internal/cache/ go.mod go.sum
git commit -m "feat: add minimal valkey cache client"
```

---

## Task 5: internal/iris — 축소 h2c 클라이언트

**Files:**
- Create: `~/gemini/settlement-go/internal/iris/client.go`

- [ ] **Step 1: client.go 작성**

`SendMessage`만 지원하는 축소 Iris 클라이언트. `httputil`/`irisx` 의존 제거, 상수 인라인.

```go
package iris

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/http2"
)

const (
	pathReply      = "/reply"
	headerBotToken = "X-Bot-Token"

	defaultTimeout            = 10 * time.Second
	defaultDialTimeout        = 3 * time.Second
	defaultIdleConnTimeout    = 90 * time.Second
	defaultReadIdleTimeout    = 30 * time.Second
	defaultPingTimeout        = 15 * time.Second
	defaultWriteByteTimeout   = 10 * time.Second
)

// Client: Iris 메시지 전송 인터페이스.
type Client interface {
	SendMessage(ctx context.Context, room, message string, opts ...SendOption) error
}

// SendOption: SendMessage 옵션.
type SendOption func(*sendOptions)

type sendOptions struct {
	ThreadID *string
}

// WithThreadID: 스레드 ID를 지정합니다.
func WithThreadID(id string) SendOption {
	return func(o *sendOptions) { o.ThreadID = &id }
}

func applySendOptions(opts []SendOption) sendOptions {
	var o sendOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

type replyRequest struct {
	Type     string  `json:"type"`
	Room     string  `json:"room"`
	Data     string  `json:"data"`
	ThreadID *string `json:"threadId,omitempty"`
}

// H2CClient: Iris h2c 클라이언트.
type H2CClient struct {
	baseURL  string
	botToken string
	client   *http.Client
	logger   *slog.Logger
}

// NewH2CClient: Iris h2c 클라이언트를 생성합니다.
func NewH2CClient(baseURL, botToken string, logger *slog.Logger) *H2CClient {
	baseURL = strings.TrimRight(baseURL, "/")
	if logger == nil {
		logger = slog.Default()
	}

	client := &http.Client{
		Timeout:   defaultTimeout,
		Transport: newTransport(baseURL, logger),
	}

	return &H2CClient{
		baseURL:  baseURL,
		botToken: botToken,
		client:   client,
		logger:   logger,
	}
}

func newTransport(baseURL string, logger *slog.Logger) http.RoundTripper {
	parsedURL, err := url.Parse(baseURL)
	if err != nil {
		return newHTTPTransport()
	}

	switch strings.ToLower(strings.TrimSpace(os.Getenv("IRIS_TRANSPORT"))) {
	case "http1", "http", "http/1.1":
		return newHTTPTransport()
	case "", "h2c", "http2":
	default:
		if logger != nil {
			logger.Warn("iris_client_unknown_transport",
				slog.String("transport", os.Getenv("IRIS_TRANSPORT")),
				slog.String("fallback", "h2c"),
			)
		}
	}

	if strings.EqualFold(parsedURL.Scheme, "http") {
		return newH2CTransport()
	}
	return newHTTPTransport()
}

func newHTTPTransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     defaultIdleConnTimeout,
		DialContext: (&net.Dialer{
			Timeout:   defaultDialTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}
}

func newH2CTransport() *http2.Transport {
	return &http2.Transport{
		AllowHTTP:        true,
		IdleConnTimeout:  defaultIdleConnTimeout,
		PingTimeout:      defaultPingTimeout,
		ReadIdleTimeout:  defaultReadIdleTimeout,
		WriteByteTimeout: defaultWriteByteTimeout,
		DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
			d := &net.Dialer{
				Timeout:   defaultDialTimeout,
				KeepAlive: 30 * time.Second,
			}
			return d.DialContext(ctx, network, addr)
		},
	}
}

func (c *H2CClient) SendMessage(ctx context.Context, room, message string, opts ...SendOption) error {
	o := applySendOptions(opts)
	reqBody := replyRequest{
		Type:     "text",
		Room:     room,
		Data:     message,
		ThreadID: o.ThreadID,
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(reqBody); err != nil {
		return fmt.Errorf("encode request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+pathReply, &buf)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(c.botToken) != "" {
		req.Header.Set(headerBotToken, c.botToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("post %s: %w", pathReply, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("post %s: status %d, body: %s", pathReply, resp.StatusCode, string(body))
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
```

- [ ] **Step 2: 빌드 확인**

```bash
cd ~/gemini/settlement-go
go build ./internal/iris/
```
Expected: 성공

- [ ] **Step 3: 커밋**

```bash
cd ~/gemini/settlement-go
git add internal/iris/
git commit -m "feat: add minimal iris h2c client"
```

---

## Task 6: internal/settlement — 도메인 모델 + 저장소 + 스케줄러

**Files:**
- Create: `~/gemini/settlement-go/internal/settlement/types.go`
- Create: `~/gemini/settlement-go/internal/settlement/repository.go`
- Create: `~/gemini/settlement-go/internal/settlement/scheduler.go`

- [ ] **Step 1: types.go — 기존 코드 복사**

소스: `/home/kapu/gemini/hololive-bot/hololive/settlement-go/pkg/settlement/types.go`
변경: 패키지 경로만 변경 (`pkg/settlement` → `internal/settlement`), 내용 동일.

- [ ] **Step 2: repository.go — 기존 코드 복사**

소스: `/home/kapu/gemini/hololive-bot/hololive/settlement-go/pkg/settlement/repository.go`
변경: 패키지 경로만 변경, 내용 동일.

- [ ] **Step 3: scheduler.go — cache import 변경**

소스: `/home/kapu/gemini/hololive-bot/hololive/settlement-go/pkg/settlement/scheduler.go`
변경:
- `"github.com/kapu/hololive-shared/pkg/service/cache"` → `"github.com/kapu/settlement-go/internal/cache"`
- `cache.Client` → `cache.Client` (동일 이름이지만 축소 interface)

- [ ] **Step 4: 의존성 추가 + 빌드 확인**

```bash
cd ~/gemini/settlement-go
go get github.com/jackc/pgx/v5
go build ./internal/settlement/
```
Expected: 성공

- [ ] **Step 5: 커밋**

```bash
cd ~/gemini/settlement-go
git add internal/settlement/ go.mod go.sum
git commit -m "feat: add settlement domain, repository, scheduler"
```

---

## Task 7: cmd/settlement — 메인 앱 이식

**Files:**
- Create: `~/gemini/settlement-go/cmd/settlement/main.go`
- Create: `~/gemini/settlement-go/cmd/settlement/handler.go`
- Create: `~/gemini/settlement-go/cmd/settlement/formatter.go`
- Create: `~/gemini/settlement-go/cmd/settlement/main_test.go`
- Create: `~/gemini/settlement-go/cmd/settlement/server_test.go`

- [ ] **Step 1: formatter.go — 기존 코드 복사**

소스: `/home/kapu/gemini/hololive-bot/hololive/settlement-go/cmd/settlement/formatter.go`
변경: import 경로 `github.com/kapu/settlement-go/internal/settlement`로 변경.

- [ ] **Step 2: handler.go — import 변경**

소스: `/home/kapu/gemini/hololive-bot/hololive/settlement-go/cmd/settlement/handler.go`
변경:
- `json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"` → `"encoding/json"` (stdlib)
- `"github.com/kapu/hololive-shared/pkg/iris"` → `"github.com/kapu/settlement-go/internal/iris"`
- `"github.com/kapu/settlement-go/pkg/settlement"` → `"github.com/kapu/settlement-go/internal/settlement"`

- [ ] **Step 3: main.go — import 변경 + DB 기본값 변경**

소스: `/home/kapu/gemini/hololive-bot/hololive/settlement-go/cmd/settlement/main.go`
변경:
- `"github.com/kapu/hololive-shared/pkg/iris"` → `"github.com/kapu/settlement-go/internal/iris"`
- `sharedlogging "github.com/kapu/hololive-shared/pkg/logging"` → `"github.com/kapu/settlement-go/internal/logging"`
- `sharedserver "github.com/kapu/hololive-shared/pkg/server"` → `"github.com/kapu/settlement-go/internal/server"`
- `"github.com/kapu/hololive-shared/pkg/service/cache"` → `"github.com/kapu/settlement-go/internal/cache"`
- `"github.com/kapu/settlement-go/pkg/settlement"` → `"github.com/kapu/settlement-go/internal/settlement"`
- `buildDatabaseURL()`: `POSTGRES_DB` 기본값 `"hololive"` → `"settlement"`, `POSTGRES_USER` 기본값 `"hololive_runtime"` → `"settlement_runtime"`
- `newLogger()` 변경: `sharedlogging.EnableFileLoggingWithLevel(sharedlogging.Config{...}, fileName, level)` → `logging.EnableFileLogging(logging.Config{Level: cfg.logLevel, Dir: cfg.logDir, ...}, fileName)`. 핵심: `Level` 필드를 `Config`에 직접 설정 (기존에는 `EnableFileLoggingWithLevel`이 3번째 인자로 level을 받아 주입하는 래퍼였음).

```go
// 변경 전
func newLogger(cfg appConfig) (*slog.Logger, error) {
    return sharedlogging.EnableFileLoggingWithLevel(sharedlogging.Config{
        Dir: cfg.logDir, MaxSizeMB: cfg.logMaxSizeMB, ...
    }, "settlement-bot.log", cfg.logLevel)
}

// 변경 후
func newLogger(cfg appConfig) (*slog.Logger, error) {
    return logging.EnableFileLogging(logging.Config{
        Level:      cfg.logLevel,
        Dir:        cfg.logDir,
        MaxSizeMB:  cfg.logMaxSizeMB,
        MaxBackups: cfg.logMaxBackups,
        MaxAgeDays: cfg.logMaxAgeDays,
        Compress:   cfg.logCompress,
    }, "settlement-bot.log")
}
```

- cache 초기화: `cache.NewCacheService(ctx, cacheCfg, logger)` → `cache.NewClient(ctx, cacheCfg, logger)`

- [ ] **Step 4: main_test.go — 기존 코드 복사 + 수정**

소스: `/home/kapu/gemini/hololive-bot/hololive/settlement-go/cmd/settlement/main_test.go`
변경: `newLogger` 내부 구현이 변경되므로 `TestNewLogger_CreatesSettlementLogFile`이 간접 영향. `enableFileLogging` API가 동일한 시그니처를 유지하므로 테스트 코드 자체는 변경 불필요. DB 기본값 테스트 추가:

```go
func TestLoadConfig_DefaultsToSettlementDB(t *testing.T) {
	cfg := loadConfig()
	if !strings.Contains(cfg.databaseURL, "/settlement?") {
		t.Fatalf("databaseURL should default to settlement DB, got %q", cfg.databaseURL)
	}
}
```

- [ ] **Step 5: server_test.go — 기존 코드 복사**

소스: `/home/kapu/gemini/hololive-bot/hololive/settlement-go/cmd/settlement/server_test.go`
변경: 없음 (stdlib + `golang.org/x/net/http2`만 사용).

- [ ] **Step 6: 빌드 확인**

```bash
cd ~/gemini/settlement-go
go build ./cmd/settlement/
```
Expected: 성공

- [ ] **Step 7: 테스트 실행**

```bash
cd ~/gemini/settlement-go
go test ./...
```
Expected: 모든 테스트 통과

- [ ] **Step 8: 커밋**

```bash
cd ~/gemini/settlement-go
git add cmd/settlement/ go.mod go.sum
git commit -m "feat: port settlement bot with internalized dependencies"
```

---

## Task 8: go.mod 정리 + go mod tidy

**Files:**
- Modify: `~/gemini/settlement-go/go.mod`

- [ ] **Step 1: go mod tidy**

```bash
cd ~/gemini/settlement-go
go mod tidy
```

- [ ] **Step 2: replace directive 없음 확인**

```bash
cd ~/gemini/settlement-go
grep "replace" go.mod
```
Expected: 출력 없음

- [ ] **Step 3: 의존성 수 확인**

```bash
cd ~/gemini/settlement-go
grep -c "^\t" go.mod
```
Expected: direct 6개 + indirect 수십 개 (기존 60+ indirect 대비 축소)

- [ ] **Step 4: 최종 빌드 + 테스트**

```bash
cd ~/gemini/settlement-go
go build ./...
go test ./...
```
Expected: 모두 성공

- [ ] **Step 5: 커밋**

```bash
cd ~/gemini/settlement-go
git add go.mod go.sum
git commit -m "chore: tidy go.mod, remove all replace directives"
```

---

## Task 9: Dockerfile + docker-compose.prod.yml

**Files:**
- Create: `~/gemini/settlement-go/Dockerfile`
- Create: `~/gemini/settlement-go/docker-compose.prod.yml`

- [ ] **Step 1: Dockerfile 작성**

```dockerfile
# syntax=docker/dockerfile:1.7
ARG GO_VERSION=1.26.1
ARG ALPINE_VERSION=3.23

FROM golang:${GO_VERSION}-alpine${ALPINE_VERSION} AS builder

ARG VERSION=dev

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o /dist/bin/settlement ./cmd/settlement

FROM alpine:${ALPINE_VERSION}

RUN apk add --no-cache ca-certificates tini tzdata

ENV TZ=Asia/Seoul

RUN addgroup -g 1000 appuser && \
    adduser -D -u 1000 -G appuser appuser

WORKDIR /app

COPY --from=builder --link --chown=1000:1000 /dist ./

EXPOSE 30002

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -q --spider http://127.0.0.1:30002/health || exit 1

USER appuser

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["./bin/settlement"]
```

- [ ] **Step 2: docker-compose.prod.yml 작성**

```yaml
networks:
  hololive-net:
    external: true

volumes:
  valkey-cache-socket:
    external: true

services:
  settlement-db-migrate:
    image: postgres:17-alpine
    entrypoint: ["sh", "-c"]
    command:
      - |
        until pg_isready -h holo-postgres -p 5432 -U settlement_runtime; do
          echo "Waiting for postgres..."
          sleep 2
        done
        psql -h holo-postgres -p 5432 -U settlement_runtime -d settlement -f /migrations/001_init.sql
    environment:
      PGPASSWORD: ${DB_PASSWORD}
    volumes:
      - ./scripts/migrations:/migrations:ro
    networks:
      - hololive-net
    restart: "no"

  settlement-bot:
    image: settlement-bot:prod
    build:
      context: .
      dockerfile: Dockerfile
      args:
        VERSION: ${SETTLEMENT_VERSION:-1.0.0}
    container_name: settlement-bot
    restart: always
    labels:
      deunhealth.restart.on.unhealthy: "true"
    env_file:
      - .env
    environment:
      SETTLEMENT_PORT: "30002"
      SETTLEMENT_ROOM_ID: ${SETTLEMENT_ROOM_ID:-}
      SETTLEMENT_ALLOW_ROOMS: ${SETTLEMENT_ALLOW_ROOMS:-}
      IRIS_BASE_URL: ${IRIS_BASE_URL:-http://host.docker.internal:3000}
      IRIS_BOT_TOKEN: ${IRIS_BOT_TOKEN:-}
      CACHE_SOCKET_PATH: /var/run/valkey/valkey-cache.sock
      CACHE_HOST: valkey-cache
      CACHE_PORT: 6379
      POSTGRES_HOST: holo-postgres
      POSTGRES_PORT: "5432"
      POSTGRES_DB: settlement
      POSTGRES_USER: settlement_runtime
      POSTGRES_PASSWORD: ${DB_PASSWORD}
      POSTGRES_SSLMODE: ${POSTGRES_SSLMODE:-require}
      LOG_DIR: /app/logs
      TZ: Asia/Seoul
    ports:
      - "${SETTLEMENT_PORT_BIND_IP:-127.0.0.1}:30002:30002"
    volumes:
      - ./logs:/app/logs
      - valkey-cache-socket:/var/run/valkey:ro
    extra_hosts:
      - "host.docker.internal:host-gateway"
    depends_on:
      settlement-db-migrate:
        condition: service_completed_successfully
    deploy:
      resources:
        limits:
          memory: 128m
    healthcheck:
      test: ["CMD-SHELL", "wget -q --spider -T 5 http://localhost:30002/health || exit 1"]
      interval: 30s
      timeout: 5s
      retries: 3
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
    security_opt:
      - no-new-privileges:true
    read_only: true
    tmpfs:
      - /tmp:size=10m
    networks:
      hololive-net:
        aliases:
          - settlement-bot
```

- [ ] **Step 3: 커밋**

```bash
cd ~/gemini/settlement-go
git add Dockerfile docker-compose.prod.yml
git commit -m "feat: add Dockerfile and docker-compose.prod.yml"
```

---

## Task 10: DB 스크립트 — migration + seed + 데이터 이관

**Files:**
- Create: `~/gemini/settlement-go/scripts/migrations/001_init.sql`
- Create: `~/gemini/settlement-go/scripts/seed.sql`
- Create: `~/gemini/settlement-go/scripts/migrate_data.sh`

- [ ] **Step 1: 001_init.sql 작성**

기존 `038_create_settlement.sql`에서 DDL + index만 추출 (seed INSERT 제외).

```sql
-- settlement DB DDL

CREATE TABLE IF NOT EXISTS settlement_members (
    id SERIAL PRIMARY KEY,
    room_id VARCHAR(64) NOT NULL,
    kakao_user_id VARCHAR(64) NOT NULL,
    member_name VARCHAR(32) NOT NULL,
    registered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (room_id, kakao_user_id),
    UNIQUE (room_id, member_name)
);

CREATE TABLE IF NOT EXISTS settlement_cycles (
    id SERIAL PRIMARY KEY,
    room_id VARCHAR(64) NOT NULL,
    year INT NOT NULL,
    month INT NOT NULL,
    total_amount INT NOT NULL DEFAULT 144000,
    per_person INT NOT NULL DEFAULT 36000,
    due_day INT NOT NULL DEFAULT 18,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (room_id, year, month)
);

CREATE TABLE IF NOT EXISTS settlement_payments (
    id SERIAL PRIMARY KEY,
    cycle_id INT NOT NULL REFERENCES settlement_cycles(id),
    member_id INT NOT NULL REFERENCES settlement_members(id),
    paid_at TIMESTAMPTZ,
    UNIQUE (cycle_id, member_id)
);

CREATE INDEX IF NOT EXISTS idx_sp_unpaid ON settlement_payments (cycle_id) WHERE paid_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_sm_room ON settlement_members (room_id);
CREATE INDEX IF NOT EXISTS idx_sc_room ON settlement_cycles (room_id);
```

- [ ] **Step 2: seed.sql 작성**

기존 `038_create_settlement.sql`에서 seed INSERT만 추출. 개발/스테이징 환경용.

```sql
-- 개발환경 seed 데이터

-- 본방
INSERT INTO settlement_members (room_id, kakao_user_id, member_name) VALUES
    ('18477992485199234', '8642621779712097478', '비포'),
    ('18477992485199234', '5586681819923009045', '심심이'),
    ('18477992485199234', '8606832258048946503', '돈이좋아요'),
    ('18477992485199234', '8169841608548180227', '겜데브')
ON CONFLICT DO NOTHING;

-- 테스트방
INSERT INTO settlement_members (room_id, kakao_user_id, member_name) VALUES
    ('451788135895779', '8642621779712097478', '비포'),
    ('451788135895779', '5586681819923009045', '심심이'),
    ('451788135895779', '8606832258048946503', '돈이좋아요'),
    ('451788135895779', '8169841608548180227', '겜데브')
ON CONFLICT DO NOTHING;

-- 테스트방2
INSERT INTO settlement_members (room_id, kakao_user_id, member_name) VALUES
    ('18476130232878491', '8642621779712097478', '비포'),
    ('18476130232878491', '5586681819923009045', '심심이'),
    ('18476130232878491', '8606832258048946503', '돈이좋아요'),
    ('18476130232878491', '8169841608548180227', '겜데브'),
    ('18476130232878491', '8307789960528895140', '박준우')
ON CONFLICT DO NOTHING;
```

- [ ] **Step 3: migrate_data.sh 작성**

스펙 문서의 `migrate_data.sh` 내용 그대로 작성 (백업 + 이관 + 시퀀스 리셋 + 검증 포함).

```bash
chmod +x scripts/migrate_data.sh
```

- [ ] **Step 4: 커밋**

```bash
cd ~/gemini/settlement-go
git add scripts/
git commit -m "feat: add DB migration, seed, and data migration scripts"
```

---

## Task 11: hololive-bot 정리

**Files:**
- Modify: `/home/kapu/gemini/hololive-bot/docker-compose.prod.yml` (settlement-bot 블록 삭제)
- Delete: `/home/kapu/gemini/hololive-bot/hololive/settlement-go/` (전체 디렉토리)

- [ ] **Step 1: docker-compose.prod.yml에서 settlement-bot 서비스 삭제**

`docker-compose.prod.yml` 226~286행 삭제 (settlement-bot 서비스 블록 전체).
또한 다른 서비스의 `depends_on`에 `settlement-bot` 참조가 있으면 제거.

- [ ] **Step 2: 039_drop_settlement.sql 마이그레이션 추가**

```bash
cat > /home/kapu/gemini/hololive-bot/hololive/hololive-kakao-bot-go/scripts/migrations/039_drop_settlement.sql <<'SQL'
-- settlement 테이블 제거 (독립 레포로 이관 완료)
DROP TABLE IF EXISTS settlement_payments CASCADE;
DROP TABLE IF EXISTS settlement_cycles CASCADE;
DROP TABLE IF EXISTS settlement_members CASCADE;
SQL
```

- [ ] **Step 3: hololive/settlement-go 디렉토리 삭제**

```bash
cd /home/kapu/gemini/hololive-bot
rm -rf hololive/settlement-go/
```

- [ ] **Step 4: shared domain의 settlement 상수 정리 (dead code 확인)**

`hololive-shared/pkg/domain/command.go`에 `CommandSettlementStatus`, `CommandSettlementPaid` 등 settlement 관련 상수가 남아있을 수 있음. `50c77d0` 커밋에서 settlement command handling이 이미 제거되었으므로, 참조하는 코드가 없으면 삭제.

```bash
cd /home/kapu/gemini/hololive-bot
grep -r "Settlement" hololive/hololive-shared/pkg/domain/ hololive/hololive-kakao-bot-go/
```
참조 없으면 해당 상수 삭제.

- [ ] **Step 5: 빌드 확인 (hololive-bot 측)**

```bash
cd /home/kapu/gemini/hololive-bot/hololive/hololive-kakao-bot-go
go build ./...
```
Expected: settlement 관련 참조가 없으므로 영향 없음

- [ ] **Step 6: 커밋 (hololive-bot 레포)**

```bash
cd /home/kapu/gemini/hololive-bot
git add docker-compose.prod.yml hololive/hololive-kakao-bot-go/scripts/migrations/039_drop_settlement.sql
git rm -rf hololive/settlement-go/
git commit -m "refactor: remove settlement-go (migrated to independent repo)"
```

---

## Task 12: DB 생성 + 데이터 이관 (인프라 — 수동 실행)

> 이 태스크는 프로덕션 인프라 변경이므로 수동 실행 가이드입니다.

- [ ] **Step 1: settlement DB + 유저 생성**

```bash
psql -h localhost -p 5433 -U postgres <<'SQL'
CREATE USER settlement_runtime WITH PASSWORD '<비밀번호>';
CREATE DATABASE settlement OWNER settlement_runtime;
GRANT ALL PRIVILEGES ON DATABASE settlement TO settlement_runtime;
SQL
```

- [ ] **Step 2: DDL 적용**

```bash
cd ~/gemini/settlement-go
psql -h localhost -p 5433 -U settlement_runtime -d settlement \
  -f scripts/migrations/001_init.sql
```

- [ ] **Step 3: 데이터 이관 실행**

```bash
cd ~/gemini/settlement-go
SRC_DB=hololive DST_DB=settlement PG_HOST=localhost PG_PORT=5433 \
  bash scripts/migrate_data.sh
```
Expected: 테이블별 row count 일치 메시지

- [ ] **Step 4: 이관 검증**

```bash
psql -h localhost -p 5433 -d settlement -c "SELECT COUNT(*) FROM settlement_members;"
psql -h localhost -p 5433 -d settlement -c "SELECT COUNT(*) FROM settlement_cycles;"
psql -h localhost -p 5433 -d settlement -c "SELECT COUNT(*) FROM settlement_payments;"
```

- [ ] **Step 5: (이관 확인 후) hololive DB에서 settlement 테이블 DROP**

```bash
psql -h localhost -p 5433 -U postgres -d hololive <<'SQL'
DROP TABLE IF EXISTS settlement_payments CASCADE;
DROP TABLE IF EXISTS settlement_cycles CASCADE;
DROP TABLE IF EXISTS settlement_members CASCADE;
SQL
```

---

## 실행 순서 요약

| Task | 레포 | 의존성 |
|------|------|--------|
| 1 | settlement-go | 없음 |
| 2 | settlement-go | 1 |
| 3 | settlement-go | 1 |
| 4 | settlement-go | 1 |
| 5 | settlement-go | 1 |
| 6 | settlement-go | 1, 4 |
| 7 | settlement-go | 2, 3, 4, 5, 6 |
| 8 | settlement-go | 7 |
| 9 | settlement-go | 8 |
| 10 | settlement-go | 1 |
| 11 | hololive-bot | 8 (settlement-go 빌드 확인 후) |
| 12 | 인프라 | 10, 11 |

**병렬 가능**: Task 2, 3, 4, 5는 서로 독립적이므로 병렬 실행 가능. Task 10도 Task 1 이후 바로 실행 가능.
