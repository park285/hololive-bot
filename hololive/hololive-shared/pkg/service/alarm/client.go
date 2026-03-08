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
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/httputil"
	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// Client: alarm CRUD HTTP 클라이언트 (domain.AlarmCRUD 구현)
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	logger     *slog.Logger

	// targetMinutes: 알림 시간 목표값 로컬 캐시
	targetMinutesMu sync.RWMutex
	targetMinutes   []int
}

var _ domain.AlarmCRUD = (*Client)(nil)

// NewClient: alarm CRUD 클라이언트를 생성합니다.
func NewClient(baseURL string, logger *slog.Logger) *Client {
	return NewClientWithAPIKey(baseURL, "", logger)
}

// NewClientWithAPIKey: alarm CRUD 클라이언트를 생성합니다. apiKey가 있으면 X-API-Key 헤더를 포함합니다.
func NewClientWithAPIKey(baseURL, apiKey string, logger *slog.Logger) *Client {
	baseURL = strings.TrimRight(baseURL, "/")
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		baseURL:    baseURL,
		apiKey:     strings.TrimSpace(apiKey),
		httpClient: httputil.NewInternalServiceClient(10 * time.Second),
		logger:     logger,
	}
}

// --- 요청 / 응답 DTO ---

// addAlarmReq: AddAlarm HTTP 요청 바디
type addAlarmReq struct {
	RoomID     string            `json:"room_id"`
	UserID     string            `json:"user_id"`
	ChannelID  string            `json:"channel_id"`
	MemberName string            `json:"member_name"`
	RoomName   string            `json:"room_name"`
	UserName   string            `json:"user_name"`
	AlarmTypes domain.AlarmTypes `json:"alarm_types"`
}

// removeAlarmReq: RemoveAlarm HTTP 요청 바디
type removeAlarmReq struct {
	RoomID     string            `json:"room_id"`
	ChannelID  string            `json:"channel_id"`
	AlarmTypes domain.AlarmTypes `json:"alarm_types"`
}

// clearRoomReq: ClearRoomAlarms HTTP 요청 바디
type clearRoomReq struct {
	RoomID string `json:"room_id"`
}

// setRoomNameReq: SetRoomName HTTP 요청 바디
type setRoomNameReq struct {
	RoomID   string `json:"room_id"`
	RoomName string `json:"room_name"`
}

// setUserNameReq: SetUserName HTTP 요청 바디
type setUserNameReq struct {
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
}

// updateAdvanceMinutesReq: UpdateAlarmAdvanceMinutes HTTP 요청 바디
type updateAdvanceMinutesReq struct {
	Minutes int `json:"minutes"`
}

// boolResp: bool 단일 값 응답
type boolResp struct {
	Result bool `json:"result"`
}

// intResp: int 단일 값 응답
type intResp struct {
	Count int `json:"count"`
}

// minutesResp: []int 응답
type minutesResp struct {
	Minutes []int `json:"minutes"`
}

// --- 인터페이스 메서드 구현 ---

// AddAlarm: POST /internal/alarm/add — 알람 등록
func (c *Client) AddAlarm(ctx context.Context, req domain.AddAlarmRequest) (bool, error) {
	body := addAlarmReq{
		RoomID:     req.RoomID,
		UserID:     req.UserID,
		ChannelID:  req.ChannelID,
		MemberName: req.MemberName,
		RoomName:   req.RoomName,
		UserName:   req.UserName,
		AlarmTypes: req.AlarmTypes,
	}
	var resp boolResp
	if err := c.postJSON(ctx, "/internal/alarm/add", body, &resp); err != nil {
		return false, err
	}
	return resp.Result, nil
}

// RemoveAlarm: POST /internal/alarm/remove — 알람 해제
func (c *Client) RemoveAlarm(ctx context.Context, roomID, channelID string, alarmTypes domain.AlarmTypes) (bool, error) {
	body := removeAlarmReq{
		RoomID:     roomID,
		ChannelID:  channelID,
		AlarmTypes: alarmTypes,
	}
	var resp boolResp
	if err := c.postJSON(ctx, "/internal/alarm/remove", body, &resp); err != nil {
		return false, err
	}
	return resp.Result, nil
}

// GetRoomAlarms: GET /internal/alarm/room/:id — 방의 채널 ID 목록 조회
// 내부적으로 GetRoomAlarmsWithTypes를 호출해 channelID만 추출합니다.
func (c *Client) GetRoomAlarms(ctx context.Context, roomID string) ([]string, error) {
	alarms, err := c.GetRoomAlarmsWithTypes(ctx, roomID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(alarms))
	for _, a := range alarms {
		ids = append(ids, a.ChannelID)
	}
	return ids, nil
}

// GetRoomAlarmsWithTypes: GET /internal/alarm/room/:id — 방의 알람 목록(타입 포함) 조회
func (c *Client) GetRoomAlarmsWithTypes(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	var alarms []*domain.Alarm
	if err := c.getJSON(ctx, "/internal/alarm/room/"+roomID, &alarms); err != nil {
		return nil, err
	}
	if alarms == nil {
		alarms = []*domain.Alarm{}
	}
	return alarms, nil
}

type apiEnvelope struct {
	Success bool             `json:"success"`
	Message string           `json:"message,omitempty"`
	Data    *json.RawMessage `json:"data,omitempty"`
}

// ListRoomAlarmsView: GET /internal/alarm/room/:id/view — 방의 알람 목록 표시용 조합 조회
func (c *Client) ListRoomAlarmsView(ctx context.Context, roomID string) ([]domain.AlarmListView, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/internal/alarm/room/"+roomID+"/view", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("alarm-api: /internal/alarm/room/%s/view: new request: %w", roomID, err)
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alarm-api: /internal/alarm/room/%s/view: %w", roomID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := httputil.CheckStatus(resp); err != nil {
		return nil, fmt.Errorf("alarm-api: /internal/alarm/room/%s/view: check status: %w", roomID, err)
	}

	var envelope apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("alarm-api: /internal/alarm/room/%s/view: decode envelope: %w", roomID, err)
	}
	if !envelope.Success {
		return nil, fmt.Errorf("alarm-api: /internal/alarm/room/%s/view: %s", roomID, envelope.Message)
	}
	if envelope.Data == nil {
		return []domain.AlarmListView{}, nil
	}

	var entries []domain.AlarmListView
	if err := json.Unmarshal(*envelope.Data, &entries); err != nil {
		return nil, fmt.Errorf("alarm-api: /internal/alarm/room/%s/view: decode entries: %w", roomID, err)
	}
	if entries == nil {
		return []domain.AlarmListView{}, nil
	}
	return entries, nil
}

// ClearRoomAlarms: POST /internal/alarm/clear — 방의 모든 알람 삭제
func (c *Client) ClearRoomAlarms(ctx context.Context, roomID string) (int, error) {
	body := clearRoomReq{RoomID: roomID}
	var resp intResp
	if err := c.postJSON(ctx, "/internal/alarm/clear", body, &resp); err != nil {
		return 0, err
	}
	return resp.Count, nil
}

// GetNextStreamInfo: GET /internal/alarm/next-stream/:id — 다음 방송 정보 조회
// 서버에 데이터가 없으면 nil을 반환합니다.
func (c *Client) GetNextStreamInfo(ctx context.Context, channelID string) (*domain.NextStreamInfo, error) {
	var info domain.NextStreamInfo
	err := c.getJSON(ctx, "/internal/alarm/next-stream/"+channelID, &info)
	if err != nil {
		// 404(데이터 없음)는 nil 반환으로 처리
		if strings.Contains(err.Error(), "status 404") {
			return nil, nil
		}
		return nil, err
	}
	// 상태값이 없으면 빈 응답으로 간주
	if !info.Status.IsValid() {
		return nil, nil
	}
	return &info, nil
}

// UpdateAlarmAdvanceMinutes: PUT /internal/alarm/settings — 알림 사전 시간 업데이트
// 에러 시 빈 슬라이스를 반환합니다.
func (c *Client) UpdateAlarmAdvanceMinutes(ctx context.Context, minutes int) []int {
	body := updateAdvanceMinutesReq{Minutes: minutes}
	var resp minutesResp
	if ctx == nil {
		c.logger.Warn("UpdateAlarmAdvanceMinutes skipped: nil context", slog.Int("minutes", minutes))
		return []int{}
	}
	if err := c.putJSON(ctx, "/internal/alarm/settings", body, &resp); err != nil {
		c.logger.Warn("UpdateAlarmAdvanceMinutes 실패",
			slog.Int("minutes", minutes),
			slog.Any("error", err),
		)
		return []int{}
	}
	c.targetMinutesMu.Lock()
	c.targetMinutes = resp.Minutes
	c.targetMinutesMu.Unlock()
	return resp.Minutes
}

// GetTargetMinutes: 로컬 캐시에서 알림 목표 시간 목록을 반환합니다.
func (c *Client) GetTargetMinutes() []int {
	c.targetMinutesMu.RLock()
	defer c.targetMinutesMu.RUnlock()
	if len(c.targetMinutes) == 0 {
		return []int{}
	}
	result := make([]int, len(c.targetMinutes))
	copy(result, c.targetMinutes)
	return result
}

// SetRoomName: PUT /internal/alarm/room-name — 방 이름 설정
func (c *Client) SetRoomName(ctx context.Context, roomID, roomName string) error {
	body := setRoomNameReq{RoomID: roomID, RoomName: roomName}
	return c.putJSON(ctx, "/internal/alarm/room-name", body, nil)
}

// SetUserName: PUT /internal/alarm/user-name — 사용자 이름 설정
func (c *Client) SetUserName(ctx context.Context, userID, userName string) error {
	body := setUserNameReq{UserID: userID, UserName: userName}
	return c.putJSON(ctx, "/internal/alarm/user-name", body, nil)
}

// GetAllAlarmKeys: GET /internal/alarm/keys — 관리자 대시보드용 전체 알람 엔트리 조회
func (c *Client) GetAllAlarmKeys(ctx context.Context) ([]*domain.AlarmEntry, error) {
	var entries []*domain.AlarmEntry
	if err := c.getJSON(ctx, "/internal/alarm/keys", &entries); err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []*domain.AlarmEntry{}
	}
	return entries, nil
}

// WarmCacheFromDB: no-op — alarm API owner 시작 시 자동으로 캐시 워밍이 수행됩니다.
func (c *Client) WarmCacheFromDB(_ context.Context) error {
	return nil
}

// --- HTTP 헬퍼 ---

// postJSON: POST 요청을 전송하고 JSON 응답을 역직렬화합니다.
func (c *Client) postJSON(ctx context.Context, path string, body any, out any) error {
	return c.doJSON(ctx, http.MethodPost, path, body, out)
}

// getJSON: GET 요청을 전송하고 JSON 응답을 역직렬화합니다.
func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, out)
}

// putJSON: PUT 요청을 전송하고 JSON 응답을 역직렬화합니다.
func (c *Client) putJSON(ctx context.Context, path string, body any, out any) error {
	return c.doJSON(ctx, http.MethodPut, path, body, out)
}

// doJSON: HTTP 요청을 전송하고 응답을 처리하는 공통 헬퍼
func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var bodyReader io.Reader
	if body != nil {
		var buf bytes.Buffer
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return fmt.Errorf("alarm-api: %s: encode request: %w", path, err)
		}
		bodyReader = &buf
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("alarm-api: %s: new request: %w", path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("alarm-api: %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := httputil.CheckStatus(resp); err != nil {
		return fmt.Errorf("alarm-api: %s: check status: %w", path, err)
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	if err := httputil.DecodeJSON(resp, out); err != nil {
		return fmt.Errorf("alarm-api: %s: decode response: %w", path, err)
	}
	return nil
}
