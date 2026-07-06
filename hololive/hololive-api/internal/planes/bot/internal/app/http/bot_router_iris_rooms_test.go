package apphttp

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	commoncontracts "github.com/kapu/hololive-shared/pkg/contracts/common"
	irisroomscontracts "github.com/kapu/hololive-shared/pkg/contracts/irisrooms"
	"github.com/park285/iris-client-go/iris"
)

type stubIrisRoomLister struct {
	resp  *iris.RoomListResponse
	err   error
	calls int
}

func (s *stubIrisRoomLister) GetRooms(context.Context) (*iris.RoomListResponse, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func TestProvideBotRouterIrisRoomsRequiresAPIKey(t *testing.T) {
	t.Parallel()

	roomType := "OM"
	name := "운영방"
	lister := &stubIrisRoomLister{resp: &iris.RoomListResponse{Rooms: []iris.RoomSummary{
		{ChatID: 123, Type: &roomType, LinkName: &name},
	}}}
	router, err := ProvideBotRouter(t.Context(), &config.Config{
		Server: config.ServerConfig{APIKey: "secret"},
	}, slog.New(slog.DiscardHandler), nil, nil, lister)
	if err != nil {
		t.Fatalf("ProvideBotRouter() error = %v", err)
	}

	noAuth := httptest.NewRecorder()
	router.ServeHTTP(noAuth, httptest.NewRequestWithContext(t.Context(), http.MethodGet, irisroomscontracts.ListPath, http.NoBody))
	if noAuth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", noAuth.Code, http.StatusUnauthorized)
	}
	if lister.calls != 0 {
		t.Fatalf("lister calls after unauthorized request = %d, want 0", lister.calls)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, irisroomscontracts.ListPath, http.NoBody)
	req.Header.Set(commoncontracts.APIKeyHeader, "secret")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("authorized status = %d, want %d body=%s", res.Code, http.StatusOK, res.Body.String())
	}
	var got iris.RoomListResponse
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Rooms) != 1 || got.Rooms[0].ChatID != 123 {
		t.Fatalf("rooms = %+v, want chatId 123", got.Rooms)
	}
}

func TestProvideBotRouterIrisRoomsFailure(t *testing.T) {
	t.Parallel()

	router, err := ProvideBotRouter(t.Context(), &config.Config{
		Server: config.ServerConfig{APIKey: "secret"},
	}, slog.New(slog.DiscardHandler), nil, nil, &stubIrisRoomLister{err: errors.New("iris down")})
	if err != nil {
		t.Fatalf("ProvideBotRouter() error = %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, irisroomscontracts.ListPath, http.NoBody)
	req.Header.Set(commoncontracts.APIKeyHeader, "secret")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d body=%s", res.Code, http.StatusBadGateway, res.Body.String())
	}
}

func TestProvideBotRouterIrisRoomsNilListerDoesNotRegisterRoute(t *testing.T) {
	t.Parallel()

	router, err := ProvideBotRouter(t.Context(), &config.Config{
		Server: config.ServerConfig{APIKey: "secret"},
	}, slog.New(slog.DiscardHandler), nil, nil, nil)
	if err != nil {
		t.Fatalf("ProvideBotRouter() error = %v", err)
	}

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, irisroomscontracts.ListPath, http.NoBody)
	req.Header.Set(commoncontracts.APIKeyHeader, "secret")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusNotFound)
	}
}
