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

package botruntime

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"

	appbootstrap "github.com/kapu/hololive-api/internal/planes/bot/internal/app/bootstrap"
)

type testAlarmCRUD struct{}

func (testAlarmCRUD) AddAlarm(context.Context, *domain.AddAlarmRequest) (bool, error) {
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

func TestBuildBotServer_IsIngressOnly(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	appConfig := &config.Config{
		Server: config.ServerConfig{Port: 30001},
	}

	server, err := appbootstrap.BuildBotServer(t.Context(), appConfig, nil, nil, nil, logger)
	if err != nil {
		t.Fatalf("buildBotServer() error = %v", err)
	}

	tests := []struct {
		name string
		path string
	}{
		{name: "admin route hidden", path: "/api/holo/members"},
		{name: "internal alarm route hidden", path: "/internal/alarm/keys"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, http.NoBody)
			w := httptest.NewRecorder()
			server.Handler.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
			}
		})
	}
}
