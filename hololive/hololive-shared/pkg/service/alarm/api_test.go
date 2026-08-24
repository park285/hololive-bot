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

package alarm

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// 테스트용 domain.AlarmCRUD mock.
type mockAlarmCRUD struct {
	addAlarmFn                  func(ctx context.Context, req domain.AddAlarmRequest) (bool, error)
	removeAlarmFn               func(ctx context.Context, roomID, channelID string, alarmTypes domain.AlarmTypes) (bool, error)
	getRoomAlarmsFn             func(ctx context.Context, roomID string) ([]string, error)
	getRoomAlarmsWithTypesFn    func(ctx context.Context, roomID string) ([]*domain.Alarm, error)
	listRoomAlarmsViewFn        func(ctx context.Context, roomID string) ([]domain.AlarmListView, error)
	clearRoomAlarmsFn           func(ctx context.Context, roomID string) (int, error)
	getNextStreamInfoFn         func(ctx context.Context, channelID string) (*domain.NextStreamInfo, error)
	updateAlarmAdvanceMinutesFn func(minutes int) []int
	getTargetMinutesFn          func() []int
	setRoomNameFn               func(ctx context.Context, roomID, roomName string) error
	setUserNameFn               func(ctx context.Context, userID, userName string) error
	getAllAlarmKeysFn           func(ctx context.Context) ([]*domain.AlarmEntry, error)
	warmCacheFromDBFn           func(ctx context.Context) error
}

func (m *mockAlarmCRUD) AddAlarm(ctx context.Context, req *domain.AddAlarmRequest) (bool, error) {
	out, err := m.addAlarmFn(ctx, *req)
	if err != nil {
		return out, fmt.Errorf("add alarm fn: %w", err)
	}

	return out, nil
}

func (m *mockAlarmCRUD) RemoveAlarm(ctx context.Context, roomID, channelID string, alarmTypes domain.AlarmTypes) (bool, error) {
	out, err := m.removeAlarmFn(ctx, roomID, channelID, alarmTypes)
	if err != nil {
		return out, fmt.Errorf("remove alarm fn: %w", err)
	}

	return out, nil
}

func (m *mockAlarmCRUD) GetRoomAlarms(ctx context.Context, roomID string) ([]string, error) {
	out, err := m.getRoomAlarmsFn(ctx, roomID)
	if err != nil {
		return out, fmt.Errorf("get room alarms fn: %w", err)
	}

	return out, nil
}

func (m *mockAlarmCRUD) GetRoomAlarmsWithTypes(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	out, err := m.getRoomAlarmsWithTypesFn(ctx, roomID)
	if err != nil {
		return out, fmt.Errorf("get room alarms with types fn: %w", err)
	}

	return out, nil
}

func (m *mockAlarmCRUD) ListRoomAlarmsView(ctx context.Context, roomID string) ([]domain.AlarmListView, error) {
	out, err := m.listRoomAlarmsViewFn(ctx, roomID)
	if err != nil {
		return out, fmt.Errorf("list room alarms view fn: %w", err)
	}

	return out, nil
}

func (m *mockAlarmCRUD) ClearRoomAlarms(ctx context.Context, roomID string) (int, error) {
	out, err := m.clearRoomAlarmsFn(ctx, roomID)
	if err != nil {
		return out, fmt.Errorf("clear room alarms fn: %w", err)
	}

	return out, nil
}

func (m *mockAlarmCRUD) GetNextStreamInfo(ctx context.Context, channelID string) (*domain.NextStreamInfo, error) {
	out, err := m.getNextStreamInfoFn(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("get next stream info fn: %w", err)
	}

	return out, nil
}

func (m *mockAlarmCRUD) UpdateAlarmAdvanceMinutes(_ context.Context, minutes int) []int {
	return m.updateAlarmAdvanceMinutesFn(minutes)
}

func (m *mockAlarmCRUD) GetTargetMinutes() []int {
	return m.getTargetMinutesFn()
}

func (m *mockAlarmCRUD) SetRoomName(ctx context.Context, roomID, roomName string) error {
	if err := m.setRoomNameFn(ctx, roomID, roomName); err != nil {
		return fmt.Errorf("set room name fn: %w", err)
	}

	return nil
}

func (m *mockAlarmCRUD) SetUserName(ctx context.Context, userID, userName string) error {
	if err := m.setUserNameFn(ctx, userID, userName); err != nil {
		return fmt.Errorf("set user name fn: %w", err)
	}

	return nil
}

func (m *mockAlarmCRUD) GetAllAlarmKeys(ctx context.Context) ([]*domain.AlarmEntry, error) {
	out, err := m.getAllAlarmKeysFn(ctx)
	if err != nil {
		return out, fmt.Errorf("get all alarm keys fn: %w", err)
	}

	return out, nil
}

func (m *mockAlarmCRUD) WarmCacheFromDB(ctx context.Context) error {
	if err := m.warmCacheFromDBFn(ctx); err != nil {
		return fmt.Errorf("warm cache from DB fn: %w", err)
	}

	return nil
}

// newTestHandler: 테스트용 핸들러와 gin.Engine을 생성합니다.
func newTestHandler(t *testing.T, mock *mockAlarmCRUD) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	logger := slog.New(slog.DiscardHandler)
	h := NewHandler(mock, logger)

	r := gin.New()
	h.RegisterRoutes(&r.RouterGroup)

	return r
}

// jsonBody: 구조체를 JSON 바이트 버퍼로 변환합니다.
func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()

	b, err := jsonv2.Marshal(v)
	require.NoError(t, err)

	return bytes.NewBuffer(b)
}

// decodeResponse: 응답 바디를 APIResponse로 디코딩합니다.
func decodeResponse(t *testing.T, body *bytes.Buffer) APIResponse {
	t.Helper()

	var resp APIResponse

	if err := jsonv2.UnmarshalRead(body, &resp); err != nil {
		t.Fatalf("응답 디코딩 실패: %v", err)
	}

	return resp
}

func TestAddAlarm(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		mockFn     func(ctx context.Context, req domain.AddAlarmRequest) (bool, error)
		wantStatus int
		wantOK     bool
		wantError  string
	}{
		{
			name: "성공",
			body: AddAlarmRequest{
				RoomID:    testRoomID,
				ChannelID: testChannelID,
			},
			mockFn: func(_ context.Context, _ domain.AddAlarmRequest) (bool, error) {
				return true, nil
			},
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
		{
			name:       "binding 실패: room_id 누락",
			body:       AddAlarmRequest{ChannelID: testChannelID},
			mockFn:     nil,
			wantStatus: http.StatusBadRequest,
			wantOK:     false,
			wantError:  "invalid_request_body",
		},
		{
			name: "서비스 에러",
			body: AddAlarmRequest{
				RoomID:    testRoomID,
				ChannelID: testChannelID,
			},
			mockFn: func(_ context.Context, _ domain.AddAlarmRequest) (bool, error) {
				return false, errors.New("db error")
			},
			wantStatus: http.StatusInternalServerError,
			wantOK:     false,
			wantError:  "alarm_add_failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAlarmCRUD{}

			if tt.mockFn != nil {
				mock.addAlarmFn = tt.mockFn
			}

			r := newTestHandler(t, mock)

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/internal/alarm/add", jsonBody(t, tt.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			resp := decodeResponse(t, rec.Body)
			if resp.Success != tt.wantOK {
				t.Errorf("success = %v, want %v", resp.Success, tt.wantOK)
			}

			if resp.Error != tt.wantError {
				t.Errorf("error = %q, want %q", resp.Error, tt.wantError)
			}
		})
	}
}

func TestRemoveAlarm(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		mockFn     func(ctx context.Context, roomID, channelID string, alarmTypes domain.AlarmTypes) (bool, error)
		wantStatus int
		wantOK     bool
	}{
		{
			name: "성공",
			body: RemoveAlarmRequest{RoomID: testRoomID, ChannelID: testChannelID},
			mockFn: func(_ context.Context, _, _ string, _ domain.AlarmTypes) (bool, error) {
				return true, nil
			},
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
		{
			name:       "binding 실패: channel_id 누락",
			body:       RemoveAlarmRequest{RoomID: testRoomID},
			wantStatus: http.StatusBadRequest,
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAlarmCRUD{}

			if tt.mockFn != nil {
				mock.removeAlarmFn = tt.mockFn
			}

			r := newTestHandler(t, mock)

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/internal/alarm/remove", jsonBody(t, tt.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			resp := decodeResponse(t, rec.Body)
			if resp.Success != tt.wantOK {
				t.Errorf("success = %v, want %v", resp.Success, tt.wantOK)
			}
		})
	}
}

func TestGetRoomAlarmsWithTypes(t *testing.T) {
	tests := []struct {
		name       string
		roomID     string
		mockFn     func(ctx context.Context, roomID string) ([]*domain.Alarm, error)
		wantStatus int
		wantOK     bool
	}{
		{
			name:   "성공",
			roomID: testRoomID,
			mockFn: func(_ context.Context, _ string) ([]*domain.Alarm, error) {
				return []*domain.Alarm{
					{RoomID: testRoomID, ChannelID: testChannelID},
				}, nil
			},
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
		{
			name:   "서비스 에러",
			roomID: "room2",
			mockFn: func(_ context.Context, _ string) ([]*domain.Alarm, error) {
				return nil, errors.New("cache miss")
			},
			wantStatus: http.StatusInternalServerError,
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAlarmCRUD{getRoomAlarmsWithTypesFn: tt.mockFn}
			r := newTestHandler(t, mock)

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/internal/alarm/room/"+tt.roomID, http.NoBody)
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			resp := decodeResponse(t, rec.Body)
			if resp.Success != tt.wantOK {
				t.Errorf("success = %v, want %v", resp.Success, tt.wantOK)
			}
		})
	}
}

func TestGetRoomAlarmsView(t *testing.T) {
	tests := []struct {
		name       string
		roomID     string
		mockFn     func(ctx context.Context, roomID string) ([]domain.AlarmListView, error)
		wantStatus int
		wantOK     bool
	}{
		{
			name:   "성공",
			roomID: testRoomID,
			mockFn: func(_ context.Context, _ string) ([]domain.AlarmListView, error) {
				return []domain.AlarmListView{{
					ChannelID:  testChannelID,
					MemberName: "Pekora",
					AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
				}}, nil
			},
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
		{
			name:   "서비스 에러",
			roomID: "room2",
			mockFn: func(_ context.Context, _ string) ([]domain.AlarmListView, error) {
				return nil, errors.New("db error")
			},
			wantStatus: http.StatusInternalServerError,
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAlarmCRUD{listRoomAlarmsViewFn: tt.mockFn}
			r := newTestHandler(t, mock)

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/internal/alarm/room/"+tt.roomID+"/view", http.NoBody)
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			resp := decodeResponse(t, rec.Body)
			if resp.Success != tt.wantOK {
				t.Errorf("success = %v, want %v", resp.Success, tt.wantOK)
			}
		})
	}
}

func TestClearRoomAlarms(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		mockFn     func(ctx context.Context, roomID string) (int, error)
		wantStatus int
		wantOK     bool
	}{
		{
			name: "성공",
			body: ClearAlarmsRequest{RoomID: testRoomID},
			mockFn: func(_ context.Context, _ string) (int, error) {
				return 3, nil
			},
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAlarmCRUD{clearRoomAlarmsFn: tt.mockFn}
			r := newTestHandler(t, mock)

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/internal/alarm/clear", jsonBody(t, tt.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			resp := decodeResponse(t, rec.Body)
			if resp.Success != tt.wantOK {
				t.Errorf("success = %v, want %v", resp.Success, tt.wantOK)
			}
		})
	}
}

func TestGetNextStreamInfo(t *testing.T) {
	sched := time.Now().Add(time.Hour)

	tests := []struct {
		name       string
		channelID  string
		mockFn     func(ctx context.Context, channelID string) (*domain.NextStreamInfo, error)
		wantStatus int
		wantOK     bool
	}{
		{
			name:      "성공",
			channelID: testChannelID,
			mockFn: func(_ context.Context, _ string) (*domain.NextStreamInfo, error) {
				return &domain.NextStreamInfo{
					Status:         domain.NextStreamStatusUpcoming,
					VideoID:        "vid1",
					Title:          "테스트 방송",
					StartScheduled: &sched,
				}, nil
			},
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
		{
			name:      "예정 방송 없음 (nil 반환)",
			channelID: "ch2",
			mockFn: func(_ context.Context, _ string) (*domain.NextStreamInfo, error) {
				var missing *domain.NextStreamInfo

				return missing, nil
			},
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
		{
			name:      "서비스 에러",
			channelID: "ch3",
			mockFn: func(_ context.Context, _ string) (*domain.NextStreamInfo, error) {
				return nil, errors.New("holodex timeout")
			},
			wantStatus: http.StatusInternalServerError,
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAlarmCRUD{getNextStreamInfoFn: tt.mockFn}
			r := newTestHandler(t, mock)

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/internal/alarm/next-stream/"+tt.channelID, http.NoBody)
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			resp := decodeResponse(t, rec.Body)
			if resp.Success != tt.wantOK {
				t.Errorf("success = %v, want %v", resp.Success, tt.wantOK)
			}
		})
	}
}

func TestUpdateAlarmAdvanceMinutes(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		mockFn     func(minutes int) []int
		wantStatus int
		wantOK     bool
	}{
		{
			name: "성공",
			body: UpdateAdvanceMinutesRequest{Minutes: 10},
			mockFn: func(_ int) []int {
				return []int{5, 10}
			},
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
		{
			name:       "binding 실패: minutes=0 (min=1 위반)",
			body:       UpdateAdvanceMinutesRequest{Minutes: 0},
			mockFn:     nil,
			wantStatus: http.StatusBadRequest,
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAlarmCRUD{}

			if tt.mockFn != nil {
				mock.updateAlarmAdvanceMinutesFn = tt.mockFn
			}

			r := newTestHandler(t, mock)

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/internal/alarm/settings", jsonBody(t, tt.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			resp := decodeResponse(t, rec.Body)
			if resp.Success != tt.wantOK {
				t.Errorf("success = %v, want %v", resp.Success, tt.wantOK)
			}
		})
	}
}

func TestGetAllAlarmKeys(t *testing.T) {
	tests := []struct {
		name       string
		mockFn     func(ctx context.Context) ([]*domain.AlarmEntry, error)
		wantStatus int
		wantOK     bool
	}{
		{
			name: "성공",
			mockFn: func(_ context.Context) ([]*domain.AlarmEntry, error) {
				return []*domain.AlarmEntry{
					{RoomID: testRoomID, ChannelID: testChannelID, MemberName: "아쿠아"},
				}, nil
			},
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAlarmCRUD{getAllAlarmKeysFn: tt.mockFn}
			r := newTestHandler(t, mock)

			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/internal/alarm/keys", http.NoBody)
			r.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			resp := decodeResponse(t, rec.Body)
			if resp.Success != tt.wantOK {
				t.Errorf("success = %v, want %v", resp.Success, tt.wantOK)
			}
		})
	}
}

func TestHealthAndReady(t *testing.T) {
	mock := &mockAlarmCRUD{}
	r := newTestHandler(t, mock)

	tests := []struct {
		name string
		path string
	}{
		{"health", "/health"},
		{"ready", "/ready"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tt.path, http.NoBody)
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("%s: status = %d, want %d", tt.name, rec.Code, http.StatusOK)
			}
		})
	}
}
