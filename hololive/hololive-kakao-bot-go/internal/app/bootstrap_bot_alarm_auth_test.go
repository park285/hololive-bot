// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package app

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"

	appbootstrap "github.com/kapu/hololive-kakao-bot-go/internal/app/bootstrap"
	botserver "github.com/kapu/hololive-kakao-bot-go/internal/server"
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
func (testAlarmCRUD) UpdateAlarmAdvanceMinutes(context.Context, int) []int { return []int{5} }
func (testAlarmCRUD) GetTargetMinutes() []int                              { return []int{5} }
func (testAlarmCRUD) SetRoomName(context.Context, string, string) error    { return nil }
func (testAlarmCRUD) SetUserName(context.Context, string, string) error    { return nil }
func (testAlarmCRUD) GetAllAlarmKeys(context.Context) ([]*domain.AlarmEntry, error) {
	return []*domain.AlarmEntry{}, nil
}
func (testAlarmCRUD) WarmCacheFromDB(context.Context) error { return nil }

func testAdminDependencies() *botAdminServerDependencies {
	apiHandler := &botserver.APIHandler{}

	return &botAdminServerDependencies{
		DomainHandlers: apiHandler.DomainHandlers(),
		AuthHandler:    &botserver.AuthHandler{},
	}
}

func TestBuildBotServer_InternalAlarmRoutesRequireAPIKey(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port:   30001,
			APIKey: "test-secret",
		},
	}

	server, err := appbootstrap.BuildBotServer(t.Context(), cfg, nil, nil, testAlarmCRUD{}, nil, logger)
	if err != nil {
		t.Fatalf("buildBotServer() error = %v", err)
	}

	t.Run("missing api key", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/internal/alarm/keys", http.NoBody)
		w := httptest.NewRecorder()
		server.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})

	t.Run("valid api key", func(t *testing.T) {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/internal/alarm/keys", http.NoBody)
		req.Header.Set("X-Api-Key", "test-secret")

		w := httptest.NewRecorder()
		server.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})
}

func TestBuildBotServer_InternalAlarmRoutesRequireConfiguredAPIKey(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	cfg := &config.Config{
		Server: config.ServerConfig{
			Port: 30001,
		},
	}

	_, err := appbootstrap.BuildBotServer(t.Context(), cfg, nil, nil, testAlarmCRUD{}, nil, logger)
	if err == nil {
		t.Fatal("buildBotServer() error = nil, want non-nil")
	}

	if !strings.Contains(err.Error(), "API_SECRET_KEY") {
		t.Fatalf("buildBotServer() error = %v, want contains API_SECRET_KEY", err)
	}
}

func TestBuildBotServer_AdminRoutesToggleByConfig(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

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

		server, err := appbootstrap.BuildBotServer(t.Context(), cfg, nil, nil, testAlarmCRUD{}, testAdminDependencies(), logger)
		if err != nil {
			t.Fatalf("buildBotServer() error = %v", err)
		}

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/holo/members", http.NoBody)
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

		server, err := appbootstrap.BuildBotServer(t.Context(), cfg, nil, nil, testAlarmCRUD{}, nil, logger)
		if err != nil {
			t.Fatalf("buildBotServer() error = %v", err)
		}

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/holo/members", http.NoBody)
		w := httptest.NewRecorder()
		server.Handler.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
		}
	})
}
