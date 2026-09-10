package observations

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"

	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/kapu/admin-dashboard/internal/contract"
)

// MaxStreams는 프로세스의 통계 WebSocket 상한입니다.
const MaxStreams = 16

// MaxStreamsPerFamily는 회전으로 우회할 수 없는 세션 family 상한입니다.
const MaxStreamsPerFamily = 4

// StatsProtocol은 현재 BFF와 브라우저가 협상해야 하는 단일 subprotocol입니다.
func StatsProtocol() string { return "admin-stats." + contract.Generation }

// StreamPolicy는 Origin·세션·오류 응답 경계를 필수로 주입합니다.
type StreamPolicy struct {
	AllowedOrigins []string
	OriginMode     config.SecurityMode
	Authorize      func(*http.Request) (string, error)
	FamilyActive   func(context.Context, string) (bool, error)
	Reject         func(http.ResponseWriter, *http.Request, error)
}

// StreamTiming은 연결 생존 감시의 시간 예산입니다.
type StreamTiming struct {
	PongWait   time.Duration
	PingPeriod time.Duration
}

// DefaultStreamTiming은 기존 60초 pong·54초 ping 계약입니다.
func DefaultStreamTiming() StreamTiming {
	return StreamTiming{PongWait: 60 * time.Second, PingPeriod: 54 * time.Second}
}

// Streams는 정책을 확인한 WebSocket의 제한·구독·종료를 소유합니다.
type Streams struct {
	hub         *Hub
	logger      *slog.Logger
	policy      StreamPolicy
	timing      StreamTiming
	mu          sync.Mutex
	closed      bool
	reserved    int
	families    map[string]int
	connections map[*websocket.Conn]struct{}
	active      sync.WaitGroup
}

// NewStreams는 필수 정책이 없으면 실패하며 외부 자원을 시작하지 않습니다.
func NewStreams(hub *Hub, logger *slog.Logger, policy StreamPolicy, timing StreamTiming) (*Streams, error) {
	if hub == nil || logger == nil || policy.Authorize == nil || policy.FamilyActive == nil || policy.Reject == nil {
		return nil, errors.New("stream policy is incomplete")
	}

	if timing.PongWait <= 0 || timing.PingPeriod <= 0 || timing.PingPeriod >= timing.PongWait {
		return nil, errors.New("invalid stream timing")
	}

	if policy.OriginMode != config.SecurityEnforce && policy.OriginMode != config.SecurityMonitor && policy.OriginMode != config.SecurityOff {
		return nil, errors.New("invalid stream origin mode")
	}

	if policy.OriginMode == config.SecurityEnforce && len(policy.AllowedOrigins) == 0 {
		return nil, errors.New("stream origins are empty")
	}

	policy.AllowedOrigins = slices.Clone(policy.AllowedOrigins)

	return &Streams{hub: hub, logger: logger, policy: policy, timing: timing, families: make(map[string]int), connections: make(map[*websocket.Conn]struct{})}, nil
}

// ServeHTTP는 세대·Origin·세션을 확인한 후에만 업그레이드하고 모든 연결 작업을 회수합니다.
func (s *Streams) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	protocols := websocket.Subprotocols(request)
	if len(protocols) != 1 || protocols[0] != StatsProtocol() {
		s.policy.Reject(w, request, &contract.AppError{Status: http.StatusConflict, Body: contract.ErrorResponse{Code: "CLIENT_GENERATION_MISMATCH", Error: "Reload the administrator application"}})

		return
	}

	if !s.originAllowed(request) {
		s.policy.Reject(w, request, contract.Forbidden())

		return
	}

	family, err := s.policy.Authorize(request)
	if err != nil {
		s.policy.Reject(w, request, err)

		return
	}

	if family == "" {
		s.policy.Reject(w, request, contract.Unauthorized())

		return
	}

	if reserveErr := s.reserve(family); reserveErr != nil {
		s.policy.Reject(w, request, reserveErr)

		return
	}

	var conn *websocket.Conn

	defer func() {
		if conn != nil {
			closeConn(conn)
		}

		s.release(family, conn)
	}()

	conn, err = s.upgrader().Upgrade(w, request, nil)
	if err != nil {
		return
	}

	if !s.attach(conn) {
		return
	}

	s.streamSystemStats(request.Context(), conn, family)
}

func (s *Streams) originAllowed(request *http.Request) bool {
	if s.policy.OriginMode == config.SecurityOff || slices.Contains(s.policy.AllowedOrigins, request.Header.Get("Origin")) {
		return true
	}

	sharedlogging.Log(request.Context(), s.logger, slog.LevelWarn, "admin.websocket_origin.denied", "WebSocket origin rejected")

	return s.policy.OriginMode == config.SecurityMonitor
}

func (s *Streams) upgrader() *websocket.Upgrader {
	return &websocket.Upgrader{
		HandshakeTimeout: 5 * time.Second, ReadBufferSize: 1024, WriteBufferSize: 4096,
		Subprotocols: []string{StatsProtocol()}, CheckOrigin: s.originAllowed,
		Error: func(w http.ResponseWriter, request *http.Request, status int, _ error) {
			s.policy.Reject(w, request, contract.NewError(status, "Invalid WebSocket handshake"))
		},
	}
}

func (s *Streams) reserve(family string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return contract.NewError(http.StatusServiceUnavailable, "Administrator service is stopping")
	}

	if s.reserved >= MaxStreams || s.families[family] >= MaxStreamsPerFamily {
		return contract.NewError(http.StatusTooManyRequests, "Too many active system stats streams")
	}

	s.reserved++

	s.families[family]++
	s.active.Add(1)

	return nil
}

func (s *Streams) attach(conn *websocket.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return false
	}

	s.connections[conn] = struct{}{}

	return true
}

func (s *Streams) release(family string, conn *websocket.Conn) {
	s.mu.Lock()

	s.reserved--

	s.families[family]--

	if s.families[family] == 0 {
		delete(s.families, family)
	}

	delete(s.connections, conn)
	s.mu.Unlock()
	s.active.Done()
}

// Close는 새 업그레이드를 막고 연결·감시·구독이 종료될 때까지 기다립니다.
func (s *Streams) Close() {
	s.mu.Lock()

	s.closed = true

	connections := make([]*websocket.Conn, 0, len(s.connections))

	for conn := range s.connections {
		connections = append(connections, conn)
	}

	s.mu.Unlock()

	for _, conn := range connections {
		closeConn(conn)
	}

	s.active.Wait()
}
