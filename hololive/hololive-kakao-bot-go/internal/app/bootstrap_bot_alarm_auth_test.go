package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	botserver "github.com/kapu/hololive-kakao-bot-go/internal/server"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type testAlarmCRUD struct{}

func (testAlarmCRUD) AddAlarm(context.Context, domain.AddAlarmRequest) (bool, error) {
	return true, nil
}
func (testAlarmCRUD) RemoveAlarm(context.Context, string, string, domain.AlarmTypes) (bool, error) {
	return true, nil
}
func (testAlarmCRUD) GetRoomAlarms(context.Context, string) ([]string, error) { return []string{}, nil }
func (testAlarmCRUD) GetRoomAlarmsWithTypes(context.Context, string) ([]*domain.Alarm, error) {
	return []*domain.Alarm{}, nil
}
func (testAlarmCRUD) ListRoomAlarmsView(context.Context, string) ([]domain.AlarmListView, error) {
	return []domain.AlarmListView{}, nil
}
func (testAlarmCRUD) ClearRoomAlarms(context.Context, string) (int, error) { return 0, nil }
func (testAlarmCRUD) GetNextStreamInfo(context.Context, string) (*domain.NextStreamInfo, error) {
	return nil, nil
}
func (testAlarmCRUD) UpdateAlarmAdvanceMinutes(int) []int               { return []int{5} }
func (testAlarmCRUD) GetTargetMinutes() []int                           { return []int{5} }
func (testAlarmCRUD) SetRoomName(context.Context, string, string) error { return nil }
func (testAlarmCRUD) SetUserName(context.Context, string, string) error { return nil }
func (testAlarmCRUD) GetAllAlarmKeys(context.Context) ([]*domain.AlarmEntry, error) {
	return []*domain.AlarmEntry{}, nil
}
func (testAlarmCRUD) WarmCacheFromDB(context.Context) error { return nil }

func testAdminDependencies() *botAdminServerDependencies {
	apiHandler := &botserver.APIHandler{}
	return &botAdminServerDependencies{
		domainHandlers: apiHandler.DomainHandlers(),
		authHandler:    &botserver.AuthHandler{},
	}
}

func TestBuildBotServer_InternalAlarmRoutesRequireAPIKey(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:   30001,
			APIKey: "test-secret",
		},
	}

	server, err := buildBotServer(context.Background(), cfg, nil, nil, testAlarmCRUD{}, nil, logger)
	if err != nil {
		t.Fatalf("buildBotServer() error = %v", err)
	}

	t.Run("missing api key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/alarm/keys", nil)
		w := httptest.NewRecorder()
		server.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("valid api key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/internal/alarm/keys", nil)
		req.Header.Set("X-API-Key", "test-secret")
		w := httptest.NewRecorder()
		server.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})
}

func TestBuildBotServer_InternalAlarmRoutesRequireConfiguredAPIKey(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port: 30001,
		},
	}

	_, err := buildBotServer(context.Background(), cfg, nil, nil, testAlarmCRUD{}, nil, logger)
	if err == nil {
		t.Fatal("buildBotServer() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "API_SECRET_KEY") {
		t.Fatalf("buildBotServer() error = %v, want contains API_SECRET_KEY", err)
	}
}

func TestBuildBotServer_AdminRoutesToggleByConfig(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("admin enabled exposes admin routes", func(t *testing.T) {
		cfg := &config.Config{
			Server: config.ServerConfig{
				Port:   30001,
				APIKey: "test-secret",
			},
			Bot: config.BotConfig{
				AdminEnabled: true,
			},
			CORS: config.CORSConfig{
				AllowedOrigins: []string{"http://localhost:3000"},
			},
		}

		server, err := buildBotServer(context.Background(), cfg, nil, nil, testAlarmCRUD{}, testAdminDependencies(), logger)
		if err != nil {
			t.Fatalf("buildBotServer() error = %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/holo/members", nil)
		w := httptest.NewRecorder()
		server.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("admin disabled hides admin routes", func(t *testing.T) {
		cfg := &config.Config{
			Server: config.ServerConfig{
				Port:   30001,
				APIKey: "test-secret",
			},
			Bot: config.BotConfig{
				AdminEnabled: false,
			},
		}

		server, err := buildBotServer(context.Background(), cfg, nil, nil, testAlarmCRUD{}, nil, logger)
		if err != nil {
			t.Fatalf("buildBotServer() error = %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/holo/members", nil)
		w := httptest.NewRecorder()
		server.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
		}
	})
}
