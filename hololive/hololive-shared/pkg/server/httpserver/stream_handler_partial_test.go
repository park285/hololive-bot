package httpserver

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

// org=all에서 일부 org만 실패하면 provider는 PartialStreamsError를 돌려주고 캐시하지 않는다(stack audit B1).
// HTTP 경계는 새 응답 필드 없이 기존 오류 매핑(500)을 유지하고 성공한 org의 부분 목록을 200으로 내보내지 않는다.
// 부분 응답을 드러내는 필드가 필요하면 공개 계약 변경이므로 별도 결정이 먼저다.
func TestGetStreamsOrgAllPartialFailureRespondsInternalErrorWithoutPartialBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mini := miniredis.RunT(t)

	service, err := holodexprovider.NewHolodexService(newPartialOrgUpstream(t).URL, "test-key", newMiniredisCache(t, mini), nil, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewHolodexService() error = %v", err)
	}

	t.Cleanup(service.Stop)

	recorder := &respondedErrorRecorder{}
	handler := &StreamHandler{Logger: slog.New(slog.DiscardHandler), Holodex: service, RespondInternalError: recorder.respond}

	assertOrgAllPartialRespondsInternalError(t, recorder, "live", handler.GetLiveStreams)
	assertOrgAllPartialRespondsInternalError(t, recorder, "upcoming", handler.GetUpcomingStreams)

	// provider의 org=all cache key는 live_streams_all, upcoming_streams_all_<hours>다.
	for _, key := range mini.Keys() {
		if strings.HasSuffix(key, "live_streams_all") || strings.Contains(key, "upcoming_streams_all_") {
			t.Fatalf("cache key %q written for a partial org=all result", key)
		}
	}
}

// newPartialOrgUpstream은 Hololive org만 성공하고 나머지 org는 404로 실패하는 Holodex 대역이다.
func newPartialOrgUpstream(t *testing.T) *httptest.Server {
	t.Helper()

	hololive := constants.HolodexAPIParams.OrgHololive
	fixture := `[{"id":"live-1","title":"Live stream","status":"live","channel_id":"channel-live",` +
		`"channel":{"id":"channel-live","name":"Live Member","org":"` + hololive + `"}}]`

	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("org") != hololive {
			http.Error(writer, "not found", http.StatusNotFound)

			return
		}

		writer.Header().Set("Content-Type", "application/json")

		if _, err := writer.Write([]byte(fixture)); err != nil {
			t.Errorf("write fixture: %v", err)
		}
	}))
	t.Cleanup(upstream.Close)

	return upstream
}

type respondedErrorRecorder struct {
	err error
}

func (r *respondedErrorRecorder) respond(c *gin.Context, userMessage, logMessage string, err error, attrs ...slog.Attr) {
	r.err = err
	RespondInternalError(slog.New(slog.DiscardHandler), c, userMessage, logMessage, err, attrs...)
}

func assertOrgAllPartialRespondsInternalError(t *testing.T, recorder *respondedErrorRecorder, route string, call func(*gin.Context)) {
	t.Helper()

	recorder.err = nil

	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)

	ctx.Request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/holo/streams/"+route+"?org=all", http.NoBody)

	call(ctx)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("%s org=all partial status = %d, want 500 (body %s)", route, response.Code, response.Body.String())
	}

	if _, ok := errors.AsType[*holodexprovider.PartialStreamsError](recorder.err); !ok {
		t.Fatalf("%s org=all responded error = %v, want *PartialStreamsError", route, recorder.err)
	}

	if body := response.Body.String(); strings.Contains(body, "live-1") || strings.Contains(body, `"streams"`) {
		t.Fatalf("%s org=all partial body = %s, want no partial stream list", route, body)
	}
}

func newMiniredisCache(t *testing.T, mini *miniredis.Miniredis) cache.Client {
	t.Helper()

	host, rawPort, err := net.SplitHostPort(mini.Addr())
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}

	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatalf("Atoi() error = %v", err)
	}

	cacheClient, err := cache.NewCacheService(t.Context(), cache.Config{
		Host:              host,
		Port:              port,
		DisableCache:      true,
		ForceSingleClient: true,
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewCacheService() error = %v", err)
	}

	t.Cleanup(func() {
		if err := cacheClient.Close(); err != nil {
			t.Errorf("cache Close() error = %v", err)
		}
	})

	return cacheClient
}
