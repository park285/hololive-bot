package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/httputil"

	"github.com/kapu/admin-dashboard/internal/adapters/docker"
	"github.com/kapu/admin-dashboard/internal/adapters/holo"
	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/kapu/admin-dashboard/internal/observations"
	"github.com/kapu/admin-dashboard/internal/session"
	"github.com/kapu/admin-dashboard/internal/static"
)

const (
	sessionIDKey  = "admin-session-id"
	sessionObjKey = "admin-session"
)

type sessionRecords interface {
	Create(ctx context.Context) (session.Session, error)
	Get(ctx context.Context, id string) (session.Session, bool, error)
	Delete(ctx context.Context, id string) error
}

type sessionStore interface {
	sessionRecords
	sessionLifecycle
	testAccountSessions
	ClaimMutation(ctx context.Context, sess session.Session, id string) (bool, error)

	Close()
}

type testAccountSessions interface {
	CurrentTestAccount(ctx context.Context) (session.TestAccount, bool, error)
	CreateTestSession(ctx context.Context, expected session.TestAccount) (session.Session, bool, error)
}

type sessionLifecycle interface {
	FamilyActive(ctx context.Context, familyID string) (bool, error)
	RevokeFamily(ctx context.Context, familyID string) error
	Refresh(ctx context.Context, id string, idle bool) (session.RefreshResult, error)
	Rotate(ctx context.Context, oldID string) (session.Session, bool, error)
}

// API는 인증·접근 분류·HTTP 응답과 관리자 WebSocket 연결을 소유합니다.
type API struct {
	cfg                     config.Config
	logger                  *slog.Logger
	sessions                sessionStore
	rateLimiter             *httputil.LoginFailureRateLimiter
	distributedLoginLimiter *session.LoginLimiter
	loginHashSlots          chan struct{}
	docker                  *docker.Client
	holo                    *holo.Client
	statusCollector         *observations.Collector
	statsHub                *observations.Hub
	streams                 *observations.Streams
	static                  static.Handler
	openapiJSON             []byte
	admission               admission
}

// Dependencies는 bootstrap에서 준비한 외부 자원과 HTTP 응답 수집기를 전달합니다.
type Dependencies struct {
	Sessions     sessionStore
	LoginLimiter *session.LoginLimiter
	Docker       *docker.Client
	Holo         *holo.Client
	Status       *observations.Collector
	Stats        *observations.Hub
	OpenAPI      []byte
}

// New는 준비된 의존성으로 HTTP 경계를 만들고 로컬 로그인 limiter를 시작합니다.
// 호출자는 Close로 로컬 자원을 해제하고 외부 store/client는 별도로 종료해야 합니다.
func New(cfg *config.Config, logger *slog.Logger, deps Dependencies) (*API, error) {
	if cfg == nil || logger == nil || deps.Sessions == nil || deps.LoginLimiter == nil || deps.Holo == nil || deps.Status == nil || deps.Stats == nil {
		return nil, errors.New("HTTP API dependencies are incomplete")
	}

	rateLimiter := httputil.NewDefaultLoginFailureRateLimiter()
	r := &API{
		cfg: *cfg, logger: logger, sessions: newCleanupSessionStore(deps.Sessions), rateLimiter: rateLimiter,
		distributedLoginLimiter: deps.LoginLimiter, loginHashSlots: newLoginHashSlots(), docker: deps.Docker, holo: deps.Holo,
		statusCollector: deps.Status, statsHub: deps.Stats, static: static.NewHandler(),
		openapiJSON: deps.OpenAPI,
	}

	streams, err := r.newStreams(observations.DefaultStreamTiming())
	if err != nil {
		return nil, fmt.Errorf("assemble stream policy: %w", err)
	}

	r.streams = streams

	rateLimiter.Start()

	return r, nil
}

// Close는 WebSocket 작업을 회수한 뒤 로컬 로그인 limiter를 종료합니다.
func (r *API) Close() {
	if r.streams != nil {
		r.streams.Close()
	}

	if r.rateLimiter != nil {
		r.rateLimiter.Stop()
	}
}

func newLoginHashSlots() chan struct{} {
	// automaxprocs가 반영한 CPU 예산 이상으로 bcrypt가 동시에 실행되어 다른 관리 요청을
	// 굶기지 않도록 대기열 없이 즉시 거부한다.
	return make(chan struct{}, max(runtime.GOMAXPROCS(0), 1))
}

func sessionIDFrom(c *gin.Context) (string, bool) {
	value := c.GetString(sessionIDKey)
	return value, value != ""
}

func sessionFrom(c *gin.Context) (*session.Session, bool) {
	value, exists := c.Get(sessionObjKey)
	if !exists {
		return nil, false
	}

	sess, ok := value.(*session.Session)

	return sess, ok && sess != nil
}
