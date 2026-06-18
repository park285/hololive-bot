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
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	json "github.com/park285/shared-go/pkg/json"
	"github.com/stretchr/testify/require"
)

// newTestClient: httptest.Server 기반 테스트 클라이언트 생성 헬퍼
func newTestClient(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, nil)
}

// writeJSON: 테스트 서버 핸들러용 JSON 응답 헬퍼
func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

type nilResponseTransport struct{}

func (nilResponseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, nil
}

func TestClientDoRequestNilResponse(t *testing.T) {
	client := NewClient("https://alarm.example", nil)
	client.httpClient.Transport = nilResponseTransport{}

	resp, err := client.doRequest(t.Context(), http.MethodGet, "/alarms", http.NoBody, false)
	if resp != nil && resp.Body != nil {
		t.Cleanup(func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				t.Fatalf("close response body: %v", closeErr)
			}
		})
	}
	if err == nil {
		t.Fatal("expected error for nil HTTP response")
	}
	if got := err.Error(); !strings.Contains(got, "nil response") {
		t.Fatalf("error = %q, want nil response context", got)
	}
}

func TestClient_AddAlarm(t *testing.T) {
	tests := []struct {
		name       string
		serverResp any
		serverCode int
		req        domain.AddAlarmRequest
		wantResult bool
		wantErr    bool
	}{
		{
			name:       "성공 - 새 알람 등록",
			serverCode: http.StatusOK,
			serverResp: boolResp{Result: true},
			req: domain.AddAlarmRequest{
				RoomID:    "room1",
				UserID:    "user1",
				ChannelID: "ch1",
			},
			wantResult: true,
		},
		{
			name:       "성공 - 이미 존재하는 알람",
			serverCode: http.StatusOK,
			serverResp: boolResp{Result: false},
			req: domain.AddAlarmRequest{
				RoomID:    "room1",
				ChannelID: "ch1",
			},
			wantResult: false,
		},
		{
			name:       "서버 에러 전파",
			serverCode: http.StatusInternalServerError,
			serverResp: map[string]string{"error": "internal"},
			req:        domain.AddAlarmRequest{RoomID: "room1", ChannelID: "ch1"},
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/internal/alarm/add", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				writeJSON(t, w, tt.serverCode, tt.serverResp)
			})
			client := newTestClient(t, mux)

			got, err := client.AddAlarm(context.Background(), &tt.req)
			if tt.wantErr {
				if err == nil {
					t.Error("error를 기대했지만 nil이 반환됨")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantResult {
				t.Errorf("result = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

func TestClient_RemoveAlarm(t *testing.T) {
	tests := []struct {
		name       string
		serverCode int
		serverResp boolResp
		wantResult bool
		wantErr    bool
	}{
		{
			name:       "성공 - 알람 해제",
			serverCode: http.StatusOK,
			serverResp: boolResp{Result: true},
			wantResult: true,
		},
		{
			name:       "성공 - 존재하지 않는 알람",
			serverCode: http.StatusOK,
			serverResp: boolResp{Result: false},
			wantResult: false,
		},
		{
			name:       "서버 에러",
			serverCode: http.StatusBadGateway,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/internal/alarm/remove", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, tt.serverCode, tt.serverResp)
			})
			client := newTestClient(t, mux)

			got, err := client.RemoveAlarm(context.Background(), "room1", "ch1", domain.AllAlarmTypes)
			if tt.wantErr {
				if err == nil {
					t.Error("error를 기대했지만 nil이 반환됨")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantResult {
				t.Errorf("result = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

func TestClient_GetRoomAlarmsWithTypes(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		serverCode int
		serverResp any
		wantLen    int
		wantErr    bool
	}{
		{
			name:       "성공 - 알람 목록 반환",
			serverCode: http.StatusOK,
			serverResp: []*domain.Alarm{
				{RoomID: "room1", ChannelID: "ch1", MemberName: "Pekora", CreatedAt: now},
				{RoomID: "room1", ChannelID: "ch2", MemberName: "Aqua", CreatedAt: now},
			},
			wantLen: 2,
		},
		{
			name:       "성공 - 빈 목록",
			serverCode: http.StatusOK,
			serverResp: []*domain.Alarm{},
			wantLen:    0,
		},
		{
			name:       "서버 에러",
			serverCode: http.StatusNotFound,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/internal/alarm/room/room1", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, tt.serverCode, tt.serverResp)
			})
			client := newTestClient(t, mux)

			got, err := client.GetRoomAlarmsWithTypes(context.Background(), "room1")
			if tt.wantErr {
				if err == nil {
					t.Error("error를 기대했지만 nil이 반환됨")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestClient_GetRoomAlarms(t *testing.T) {
	// GetRoomAlarms는 GetRoomAlarmsWithTypes를 호출해 channelID만 추출
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/alarm/room/room1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, []*domain.Alarm{
			{ChannelID: "ch1"},
			{ChannelID: "ch2"},
		})
	})
	client := newTestClient(t, mux)

	got, err := client.GetRoomAlarms(context.Background(), "room1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("len = %d, want 2", len(got))
	}
	if got[0] != "ch1" || got[1] != "ch2" {
		t.Errorf("channel ids = %v, want [ch1, ch2]", got)
	}
}

func TestClient_ListRoomAlarmsView(t *testing.T) {
	now := time.Now()
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/alarm/room/room1/view", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, APIResponse{
			Success: true,
			Data: []domain.AlarmListView{{
				ChannelID:  "ch1",
				MemberName: "Pekora",
				AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
				NextStream: &domain.NextStreamInfo{
					Status:         domain.NextStreamStatusUpcoming,
					Title:          "Test Stream",
					VideoID:        "vid1",
					StartScheduled: &now,
				},
			}},
		})
	})
	client := newTestClient(t, mux)

	got, err := client.ListRoomAlarmsView(context.Background(), "room1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].MemberName != "Pekora" {
		t.Fatalf("member_name = %q, want Pekora", got[0].MemberName)
	}
	if got[0].NextStream == nil || got[0].NextStream.VideoID != "vid1" {
		t.Fatalf("next_stream = %#v, want video_id vid1", got[0].NextStream)
	}
}

func TestClient_ListRoomAlarmsView_RequiresViewEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/alarm/room/room1/view", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	client := newTestClient(t, mux)

	if _, err := client.ListRoomAlarmsView(context.Background(), "room1"); err == nil {
		t.Fatal("error를 기대했지만 nil이 반환됨")
	}
}

func TestClient_ListRoomAlarmsView_WithHandler(t *testing.T) {
	now := time.Now()
	mock := &mockAlarmCRUD{
		listRoomAlarmsViewFn: func(_ context.Context, roomID string) ([]domain.AlarmListView, error) {
			if roomID != "room1" {
				t.Fatalf("roomID = %q, want room1", roomID)
			}
			return []domain.AlarmListView{{
				ChannelID:  "ch1",
				MemberName: "Miko",
				AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
				NextStream: &domain.NextStreamInfo{
					Status:         domain.NextStreamStatusUpcoming,
					Title:          "API Handler Stream",
					VideoID:        "api1",
					StartScheduled: &now,
				},
			}}, nil
		},
	}
	router := newTestHandler(t, mock)
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	client := NewClient(srv.URL, nil)

	got, err := client.ListRoomAlarmsView(context.Background(), "room1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].MemberName != "Miko" {
		t.Fatalf("member_name = %q, want Miko", got[0].MemberName)
	}
	if got[0].NextStream == nil || got[0].NextStream.VideoID != "api1" {
		t.Fatalf("next_stream = %#v, want video_id api1", got[0].NextStream)
	}
}

func TestClient_ClearRoomAlarms(t *testing.T) {
	tests := []struct {
		name       string
		serverCode int
		serverResp intResp
		wantCount  int
		wantErr    bool
	}{
		{
			name:       "성공 - 알람 삭제됨",
			serverCode: http.StatusOK,
			serverResp: intResp{Count: 3},
			wantCount:  3,
		},
		{
			name:       "성공 - 삭제할 알람 없음",
			serverCode: http.StatusOK,
			serverResp: intResp{Count: 0},
			wantCount:  0,
		},
		{
			name:       "서버 에러",
			serverCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/internal/alarm/clear", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, tt.serverCode, tt.serverResp)
			})
			client := newTestClient(t, mux)

			got, err := client.ClearRoomAlarms(context.Background(), "room1")
			if tt.wantErr {
				if err == nil {
					t.Error("error를 기대했지만 nil이 반환됨")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.wantCount {
				t.Errorf("count = %d, want %d", got, tt.wantCount)
			}
		})
	}
}

func TestClient_AddAlarm_WithAPIKeyHeader(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/alarm/add", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("X-API-Key"), "secret-key"; got != want {
			t.Fatalf("X-API-Key = %q, want %q", got, want)
		}
		writeJSON(t, w, http.StatusOK, boolResp{Result: true})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := NewClientWithAPIKey(srv.URL, "secret-key", nil)

	got, err := client.AddAlarm(context.Background(), &domain.AddAlarmRequest{
		RoomID:    "room1",
		UserID:    "user1",
		ChannelID: "ch1",
	})
	if err != nil {
		t.Fatalf("AddAlarm() error = %v", err)
	}
	if !got {
		t.Fatalf("AddAlarm() result = %v, want true", got)
	}
}

func TestClient_GetNextStreamInfo(t *testing.T) {
	now := time.Now().Truncate(time.Second)

	for _, tt := range nextStreamInfoClientCases(now) {
		t.Run(tt.name, func(t *testing.T) {
			runNextStreamInfoClientCase(t, tt)
		})
	}
}

type nextStreamInfoClientCase struct {
	name       string
	serverCode int
	serverResp any
	wantNil    bool
	wantStatus domain.NextStreamStatus
	wantErr    bool
}

func nextStreamInfoClientCases(now time.Time) []nextStreamInfoClientCase {
	return []nextStreamInfoClientCase{
		{
			name:       "성공 - upcoming 방송 있음",
			serverCode: http.StatusOK,
			serverResp: map[string]any{
				"Status":         "upcoming",
				"VideoID":        "vid1",
				"Title":          "테스트 방송",
				"StartScheduled": now.Format(time.RFC3339),
			},
			wantStatus: domain.NextStreamStatusUpcoming,
		},
		{
			name:       "성공 - live 방송 중",
			serverCode: http.StatusOK,
			serverResp: map[string]any{
				"Status":  "live",
				"VideoID": "vid2",
				"Title":   "라이브 방송",
			},
			wantStatus: domain.NextStreamStatusLive,
		},
		{
			name:       "404 - 데이터 없음, nil 반환",
			serverCode: http.StatusNotFound,
			serverResp: map[string]string{"error": "not found"},
			wantNil:    true,
		},
		{
			name:       "빈 응답 - nil 반환",
			serverCode: http.StatusOK,
			serverResp: map[string]any{"Status": ""},
			wantNil:    true,
		},
	}
}

func runNextStreamInfoClientCase(t *testing.T, tt nextStreamInfoClientCase) {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/internal/alarm/next-stream/ch1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, tt.serverCode, tt.serverResp)
	})
	client := newTestClient(t, mux)

	got, err := client.GetNextStreamInfo(context.Background(), "ch1")
	assertNextStreamInfoClientResult(t, got, err, tt)
}

func assertNextStreamInfoClientResult(t *testing.T, got *domain.NextStreamInfo, err error, tt nextStreamInfoClientCase) {
	t.Helper()

	if tt.wantErr {
		if err == nil {
			t.Error("error를 기대했지만 nil이 반환됨")
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tt.wantNil {
		if got != nil {
			t.Errorf("nil을 기대했지만 %+v 반환됨", got)
		}
		return
	}
	if got == nil {
		t.Fatal("non-nil을 기대했지만 nil 반환됨")
	}
	if got.Status != tt.wantStatus {
		t.Errorf("status = %s, want %s", got.Status, tt.wantStatus)
	}
}

func TestClient_UpdateAlarmAdvanceMinutes(t *testing.T) {
	tests := []struct {
		name        string
		serverCode  int
		serverResp  any
		inputMin    int
		wantMinutes []int
	}{
		{
			name:        "성공 - 목표 시간 업데이트",
			serverCode:  http.StatusOK,
			serverResp:  minutesResp{Minutes: []int{5, 3, 1}},
			inputMin:    5,
			wantMinutes: []int{5, 3, 1},
		},
		{
			name:        "서버 에러 - 빈 슬라이스 반환",
			serverCode:  http.StatusInternalServerError,
			serverResp:  map[string]string{"error": "error"},
			inputMin:    3,
			wantMinutes: []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/internal/alarm/settings", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					t.Errorf("method = %s, want PUT", r.Method)
				}
				writeJSON(t, w, tt.serverCode, tt.serverResp)
			})
			client := newTestClient(t, mux)

			got := client.UpdateAlarmAdvanceMinutes(context.Background(), tt.inputMin)
			if len(got) != len(tt.wantMinutes) {
				t.Errorf("len = %d, want %d", len(got), len(tt.wantMinutes))
				return
			}
			for i, v := range got {
				if v != tt.wantMinutes[i] {
					t.Errorf("minutes[%d] = %d, want %d", i, v, tt.wantMinutes[i])
				}
			}
		})
	}
}

func TestClient_GetTargetMinutes(t *testing.T) {
	// UpdateAlarmAdvanceMinutes 호출 전후 캐싱 동작 확인
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/alarm/settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, minutesResp{Minutes: []int{5, 3, 1}})
	})
	client := newTestClient(t, mux)

	// 초기에는 빈 슬라이스
	initial := client.GetTargetMinutes()
	if len(initial) != 0 {
		t.Errorf("초기 targetMinutes = %v, want []", initial)
	}

	// 업데이트 후 캐시 반영
	client.UpdateAlarmAdvanceMinutes(context.Background(), 5)
	got := client.GetTargetMinutes()
	if len(got) != 3 {
		t.Errorf("len = %d, want 3", len(got))
	}
}

func TestClient_SetRoomName(t *testing.T) {
	tests := []struct {
		name       string
		serverCode int
		wantErr    bool
	}{
		{name: "성공", serverCode: http.StatusOK},
		{name: "서버 에러", serverCode: http.StatusBadRequest, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/internal/alarm/room-name", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					t.Errorf("method = %s, want PUT", r.Method)
				}
				w.WriteHeader(tt.serverCode)
			})
			client := newTestClient(t, mux)

			err := client.SetRoomName(context.Background(), "room1", "방 이름")
			if tt.wantErr && err == nil {
				t.Error("error를 기대했지만 nil이 반환됨")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestClient_SetUserName(t *testing.T) {
	tests := []struct {
		name       string
		serverCode int
		wantErr    bool
	}{
		{name: "성공", serverCode: http.StatusOK},
		{name: "서버 에러", serverCode: http.StatusBadRequest, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/internal/alarm/user-name", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					t.Errorf("method = %s, want PUT", r.Method)
				}
				w.WriteHeader(tt.serverCode)
			})
			client := newTestClient(t, mux)

			err := client.SetUserName(context.Background(), "user1", "사용자 이름")
			if tt.wantErr && err == nil {
				t.Error("error를 기대했지만 nil이 반환됨")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestClient_GetAllAlarmKeys(t *testing.T) {
	tests := []struct {
		name       string
		serverCode int
		serverResp any
		wantLen    int
		wantErr    bool
	}{
		{
			name:       "성공 - 엔트리 목록 반환",
			serverCode: http.StatusOK,
			serverResp: []*domain.AlarmEntry{
				{RoomID: "r1", ChannelID: "ch1", MemberName: "Pekora"},
				{RoomID: "r2", ChannelID: "ch2", MemberName: "Aqua"},
			},
			wantLen: 2,
		},
		{
			name:       "성공 - 빈 목록",
			serverCode: http.StatusOK,
			serverResp: []*domain.AlarmEntry{},
			wantLen:    0,
		},
		{
			name:       "서버 에러",
			serverCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/internal/alarm/keys", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, tt.serverCode, tt.serverResp)
			})
			client := newTestClient(t, mux)

			got, err := client.GetAllAlarmKeys(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Error("error를 기대했지만 nil이 반환됨")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestClient_WarmCacheFromDB(t *testing.T) {
	// no-op: 에러 없이 반환되는지만 확인
	client := NewClient("http://localhost:9999", nil)
	err := client.WarmCacheFromDB(context.Background())
	if err != nil {
		t.Errorf("WarmCacheFromDB은 no-op이어야 하지만 에러 반환: %v", err)
	}
}
