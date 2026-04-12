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
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/domain"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"
)

// mockAlarmCRUD: 테스트용 domain.AlarmCRUD mock
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

func (m *mockAlarmCRUD) AddAlarm(ctx context.Context, req domain.AddAlarmRequest) (bool, error) {
	return m.addAlarmFn(ctx, req)
}

func (m *mockAlarmCRUD) RemoveAlarm(ctx context.Context, roomID, channelID string, alarmTypes domain.AlarmTypes) (bool, error) {
	return m.removeAlarmFn(ctx, roomID, channelID, alarmTypes)
}

func (m *mockAlarmCRUD) GetRoomAlarms(ctx context.Context, roomID string) ([]string, error) {
	return m.getRoomAlarmsFn(ctx, roomID)
}

func (m *mockAlarmCRUD) GetRoomAlarmsWithTypes(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	return m.getRoomAlarmsWithTypesFn(ctx, roomID)
}

func (m *mockAlarmCRUD) ListRoomAlarmsView(ctx context.Context, roomID string) ([]domain.AlarmListView, error) {
	return m.listRoomAlarmsViewFn(ctx, roomID)
}

func (m *mockAlarmCRUD) ClearRoomAlarms(ctx context.Context, roomID string) (int, error) {
	return m.clearRoomAlarmsFn(ctx, roomID)
}

func (m *mockAlarmCRUD) GetNextStreamInfo(ctx context.Context, channelID string) (*domain.NextStreamInfo, error) {
	return m.getNextStreamInfoFn(ctx, channelID)
}

func (m *mockAlarmCRUD) UpdateAlarmAdvanceMinutes(_ context.Context, minutes int) []int {
	return m.updateAlarmAdvanceMinutesFn(minutes)
}

func (m *mockAlarmCRUD) GetTargetMinutes() []int {
	return m.getTargetMinutesFn()
}

func (m *mockAlarmCRUD) SetRoomName(ctx context.Context, roomID, roomName string) error {
	return m.setRoomNameFn(ctx, roomID, roomName)
}

func (m *mockAlarmCRUD) SetUserName(ctx context.Context, userID, userName string) error {
	return m.setUserNameFn(ctx, userID, userName)
}

func (m *mockAlarmCRUD) GetAllAlarmKeys(ctx context.Context) ([]*domain.AlarmEntry, error) {
	return m.getAllAlarmKeysFn(ctx)
}

func (m *mockAlarmCRUD) WarmCacheFromDB(ctx context.Context) error {
	return m.warmCacheFromDBFn(ctx)
}

// newTestHandler: 테스트용 핸들러와 gin.Engine을 생성합니다.
func newTestHandler(t *testing.T, mock *mockAlarmCRUD) (*APIHandler, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := NewAPIHandler(mock, logger)

	r := gin.New()
	h.RegisterRoutes(&r.RouterGroup)
	return h, r
}

// jsonBody: 구조체를 JSON 바이트 버퍼로 변환합니다.
func jsonBody(v any) *bytes.Buffer {
	b, _ := json.Marshal(v)
	return bytes.NewBuffer(b)
}

// decodeResponse: 응답 바디를 APIResponse로 디코딩합니다.
func decodeResponse(t *testing.T, body *bytes.Buffer) APIResponse {
	t.Helper()
	var resp APIResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
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
	}{
		{
			name: "성공",
			body: AddAlarmRequest{
				RoomID:    "room1",
				ChannelID: "ch1",
			},
			mockFn: func(_ context.Context, _ domain.AddAlarmRequest) (bool, error) {
				return true, nil
			},
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
		{
			name:       "binding 실패: room_id 누락",
			body:       AddAlarmRequest{ChannelID: "ch1"}, // room_id 없음
			mockFn:     nil,
			wantStatus: http.StatusBadRequest,
			wantOK:     false,
		},
		{
			name: "서비스 에러",
			body: AddAlarmRequest{
				RoomID:    "room1",
				ChannelID: "ch1",
			},
			mockFn: func(_ context.Context, _ domain.AddAlarmRequest) (bool, error) {
				return false, errors.New("db error")
			},
			wantStatus: http.StatusInternalServerError,
			wantOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAlarmCRUD{}
			if tt.mockFn != nil {
				mock.addAlarmFn = tt.mockFn
			}
			_, r := newTestHandler(t, mock)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/internal/alarm/add", jsonBody(tt.body))
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
			body: RemoveAlarmRequest{RoomID: "room1", ChannelID: "ch1"},
			mockFn: func(_ context.Context, _, _ string, _ domain.AlarmTypes) (bool, error) {
				return true, nil
			},
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
		{
			name:       "binding 실패: channel_id 누락",
			body:       RemoveAlarmRequest{RoomID: "room1"},
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
			_, r := newTestHandler(t, mock)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/internal/alarm/remove", jsonBody(tt.body))
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
			roomID: "room1",
			mockFn: func(_ context.Context, _ string) ([]*domain.Alarm, error) {
				return []*domain.Alarm{
					{RoomID: "room1", ChannelID: "ch1"},
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
			_, r := newTestHandler(t, mock)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/internal/alarm/room/"+tt.roomID, nil)
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
			roomID: "room1",
			mockFn: func(_ context.Context, _ string) ([]domain.AlarmListView, error) {
				return []domain.AlarmListView{{
					ChannelID:  "ch1",
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
			_, r := newTestHandler(t, mock)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/internal/alarm/room/"+tt.roomID+"/view", nil)
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
			body: ClearAlarmsRequest{RoomID: "room1"},
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
			_, r := newTestHandler(t, mock)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/internal/alarm/clear", jsonBody(tt.body))
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
			channelID: "ch1",
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
				return nil, nil
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
			_, r := newTestHandler(t, mock)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/internal/alarm/next-stream/"+tt.channelID, nil)
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
			mockFn: func(minutes int) []int {
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
			_, r := newTestHandler(t, mock)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/internal/alarm/settings", jsonBody(tt.body))
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
					{RoomID: "room1", ChannelID: "ch1", MemberName: "아쿠아"},
				}, nil
			},
			wantStatus: http.StatusOK,
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockAlarmCRUD{getAllAlarmKeysFn: tt.mockFn}
			_, r := newTestHandler(t, mock)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/internal/alarm/keys", nil)
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
	_, r := newTestHandler(t, mock)

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
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("%s: status = %d, want %d", tt.name, rec.Code, http.StatusOK)
			}
		})
	}
}
