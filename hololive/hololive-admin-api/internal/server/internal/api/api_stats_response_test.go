package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"

	"github.com/kapu/hololive-admin-api/internal/service/system"
)

func TestStatsAPIHandler_StreamSystemStats_CollectorUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := &StatsAPIHandler{APIHandler: &APIHandler{logger: newDiscardLogger()}}
	ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/stats/system", nil)

	handler.StreamSystemStats(ctx)

	assertErrorResponse(t, rec, http.StatusBadRequest, "System stats collector not available")
}

func TestStatsAPIHandler_StreamSystemStats_WritesInitialFrameAndStopsAfterRequestContextCancel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldUpgrader := sharedserver.WSUpgrader
	sharedserver.WSUpgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(*http.Request) bool {
			return true
		},
	}
	t.Cleanup(func() {
		sharedserver.WSUpgrader = oldUpgrader
	})

	handler := &StatsAPIHandler{APIHandler: &APIHandler{
		logger:      newDiscardLogger(),
		systemStats: system.NewCollector(nil),
	}}

	done := make(chan struct{})
	router := gin.New()
	router.GET("/api/holo/stats/system", func(c *gin.Context) {
		defer close(done)
		handler.StreamSystemStats(c)
	})

	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()

	srv := httptest.NewUnstartedServer(router)
	srv.Config.BaseContext = func(net.Listener) context.Context {
		return baseCtx
	}
	srv.Start()
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/holo/stats/system"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial error: %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline error: %v", err)
	}

	var initial map[string]any
	if err := conn.ReadJSON(&initial); err != nil {
		t.Fatalf("ReadJSON initial stats error: %v", err)
	}
	if _, ok := initial["goroutines"]; !ok {
		t.Fatalf("initial stats = %#v, want goroutines field", initial)
	}

	cancelBase()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("StreamSystemStats did not return after request context cancellation")
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("websocket close error: %v", err)
	}
}

func TestStatsAPIHandler_StreamSystemStats_WritesPeriodicFrame(t *testing.T) {
	gin.SetMode(gin.TestMode)

	oldInterval := systemStatsStreamInterval
	systemStatsStreamInterval = 20 * time.Millisecond
	t.Cleanup(func() {
		systemStatsStreamInterval = oldInterval
	})

	oldUpgrader := sharedserver.WSUpgrader
	sharedserver.WSUpgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(*http.Request) bool {
			return true
		},
	}
	t.Cleanup(func() {
		sharedserver.WSUpgrader = oldUpgrader
	})

	handler := &StatsAPIHandler{APIHandler: &APIHandler{
		logger:      newDiscardLogger(),
		systemStats: system.NewCollector(nil),
	}}

	done := make(chan struct{})
	router := gin.New()
	router.GET("/api/holo/stats/system", func(c *gin.Context) {
		defer close(done)
		handler.StreamSystemStats(c)
	})

	baseCtx, cancelBase := context.WithCancel(context.Background())
	defer cancelBase()

	srv := httptest.NewUnstartedServer(router)
	srv.Config.BaseContext = func(net.Listener) context.Context {
		return baseCtx
	}
	srv.Start()
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/holo/stats/system"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial error: %v", err)
	}
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline error: %v", err)
	}

	var initial map[string]any
	if err := conn.ReadJSON(&initial); err != nil {
		t.Fatalf("ReadJSON initial stats error: %v", err)
	}
	if _, ok := initial["goroutines"]; !ok {
		t.Fatalf("initial stats = %#v, want goroutines field", initial)
	}

	var periodic map[string]any
	if err := conn.ReadJSON(&periodic); err != nil {
		t.Fatalf("ReadJSON periodic stats error: %v", err)
	}
	if _, ok := periodic["goroutines"]; !ok {
		t.Fatalf("periodic stats = %#v, want goroutines field", periodic)
	}

	cancelBase()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("StreamSystemStats did not return after request context cancellation")
	}
}
