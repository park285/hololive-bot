package api

import (
	"context"
	jsonv2 "encoding/json/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"github.com/kapu/hololive-api/internal/planes/admin/internal/service/system"
	sharedserver "github.com/kapu/hololive-shared/pkg/server/httpserver"
)

const systemStatsPath = "/api/holo/stats/system"

func readSystemStatsFrame(t *testing.T, conn *websocket.Conn, destination any) {
	t.Helper()

	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read system stats frame: %v", err)
	}

	if messageType != websocket.TextMessage {
		t.Fatalf("system stats message type = %d, want %d", messageType, websocket.TextMessage)
	}

	if err := jsonv2.Unmarshal(payload, destination); err != nil {
		t.Fatalf("decode system stats frame: %v", err)
	}
}

func TestStatsHandler_StreamSystemStats_CollectorUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := &StatsHandler{Handler: &Handler{logger: newDiscardLogger()}}
	ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/stats/system", nil)

	handler.StreamSystemStats(ctx)

	assertErrorResponse(t, rec, http.StatusBadRequest, "System stats collector not available")
}

type systemStatsStreamFixture struct {
	conn *websocket.Conn
	done <-chan struct{}

	cancelBase context.CancelFunc
}

func newSystemStatsStreamFixture(t *testing.T) *systemStatsStreamFixture {
	t.Helper()
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

	handler := &StatsHandler{Handler: &Handler{
		logger:      newDiscardLogger(),
		systemStats: system.NewCollector(nil),
	}}

	done := make(chan struct{})
	router := gin.New()
	router.GET(systemStatsPath, func(c *gin.Context) {
		defer close(done)

		handler.StreamSystemStats(c)
	})

	baseCtx, cancelBase := context.WithCancel(t.Context())
	t.Cleanup(cancelBase)

	srv := httptest.NewUnstartedServer(router)

	srv.Config.BaseContext = func(net.Listener) context.Context {
		return baseCtx
	}
	srv.Start()
	t.Cleanup(srv.Close)

	return &systemStatsStreamFixture{
		conn:       dialSystemStatsStream(t, srv.URL),
		done:       done,
		cancelBase: cancelBase,
	}
}

func dialSystemStatsStream(t *testing.T, serverURL string) *websocket.Conn {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + systemStatsPath

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil && resp.Body != nil {
		t.Cleanup(func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Errorf("response body close error: %v", closeErr)
			}
		})
	}

	if err != nil {
		t.Fatalf("websocket dial error: %v", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline error: %v", err)
	}

	return conn
}

func assertSystemStatsFrame(t *testing.T, conn *websocket.Conn, label string) {
	t.Helper()

	var frame map[string]any

	readSystemStatsFrame(t, conn, &frame)

	if _, ok := frame["goroutines"]; !ok {
		t.Fatalf("%s stats = %#v, want goroutines field", label, frame)
	}
}

func assertSystemStatsStreamStopped(t *testing.T, done <-chan struct{}) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("StreamSystemStats did not return after request context cancellation")
	}
}

func TestStatsHandler_StreamSystemStats_WritesInitialFrameAndStopsAfterRequestContextCancel(t *testing.T) {
	fixture := newSystemStatsStreamFixture(t)

	assertSystemStatsFrame(t, fixture.conn, "initial")

	fixture.cancelBase()
	assertSystemStatsStreamStopped(t, fixture.done)

	if err := fixture.conn.Close(); err != nil {
		t.Fatalf("websocket close error: %v", err)
	}
}

func TestStatsHandler_StreamSystemStats_WritesPeriodicFrame(t *testing.T) {
	oldInterval := systemStatsStreamInterval

	systemStatsStreamInterval = 20 * time.Millisecond

	t.Cleanup(func() {
		systemStatsStreamInterval = oldInterval
	})

	fixture := newSystemStatsStreamFixture(t)

	t.Cleanup(func() {
		if closeErr := fixture.conn.Close(); closeErr != nil {
			t.Errorf("websocket close error: %v", closeErr)
		}
	})

	assertSystemStatsFrame(t, fixture.conn, "initial")
	assertSystemStatsFrame(t, fixture.conn, "periodic")

	fixture.cancelBase()
	assertSystemStatsStreamStopped(t, fixture.done)
}
