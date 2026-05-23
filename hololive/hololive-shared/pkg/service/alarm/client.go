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

	"github.com/park285/hololive-bot/shared-go/pkg/httputil"
	json "github.com/park285/hololive-bot/shared-go/pkg/json"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	logger     *slog.Logger

	targetMinutesMu sync.RWMutex
	targetMinutes   []int
}

var _ domain.AlarmCRUD = (*Client)(nil)

func NewClient(baseURL string, logger *slog.Logger) *Client {
	return NewClientWithAPIKey(baseURL, "", logger)
}

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

type addAlarmReq struct {
	RoomID     string            `json:"room_id"`
	UserID     string            `json:"user_id"`
	ChannelID  string            `json:"channel_id"`
	MemberName string            `json:"member_name"`
	RoomName   string            `json:"room_name"`
	UserName   string            `json:"user_name"`
	AlarmTypes domain.AlarmTypes `json:"alarm_types"`
}

type removeAlarmReq struct {
	RoomID     string            `json:"room_id"`
	ChannelID  string            `json:"channel_id"`
	AlarmTypes domain.AlarmTypes `json:"alarm_types"`
}

type clearRoomReq struct {
	RoomID string `json:"room_id"`
}

type setRoomNameReq struct {
	RoomID   string `json:"room_id"`
	RoomName string `json:"room_name"`
}

type setUserNameReq struct {
	UserID   string `json:"user_id"`
	UserName string `json:"user_name"`
}

type updateAdvanceMinutesReq struct {
	Minutes int `json:"minutes"`
}

type boolResp struct {
	Result bool `json:"result"`
}

type intResp struct {
	Count int `json:"count"`
}

type minutesResp struct {
	Minutes []int `json:"minutes"`
}

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
	if err := c.postJSON(ctx, contractsalarm.AddPath, body, &resp); err != nil {
		return false, err
	}
	return resp.Result, nil
}

func (c *Client) RemoveAlarm(ctx context.Context, roomID, channelID string, alarmTypes domain.AlarmTypes) (bool, error) {
	body := removeAlarmReq{
		RoomID:     roomID,
		ChannelID:  channelID,
		AlarmTypes: alarmTypes,
	}
	var resp boolResp
	if err := c.postJSON(ctx, contractsalarm.RemovePath, body, &resp); err != nil {
		return false, err
	}
	return resp.Result, nil
}

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

func (c *Client) GetRoomAlarmsWithTypes(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	var alarms []*domain.Alarm
	if err := c.getJSON(ctx, contractsalarm.RoomAlarmsPath(roomID), &alarms); err != nil {
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

func (c *Client) ListRoomAlarmsView(ctx context.Context, roomID string) ([]domain.AlarmListView, error) {
	path := contractsalarm.RoomAlarmsViewPath(roomID)
	resp, err := c.doRequest(ctx, http.MethodGet, path, http.NoBody, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	return decodeAlarmListViewEnvelope(path, resp.Body)
}

func decodeAlarmListViewEnvelope(path string, body io.Reader) ([]domain.AlarmListView, error) {
	var envelope apiEnvelope
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("alarm-api: %s: decode envelope: %w", path, err)
	}
	if !envelope.Success {
		return nil, fmt.Errorf("alarm-api: %s: %s", path, envelope.Message)
	}
	if envelope.Data == nil {
		return []domain.AlarmListView{}, nil
	}

	return decodeAlarmListViewEntries(path, *envelope.Data)
}

func decodeAlarmListViewEntries(path string, data json.RawMessage) ([]domain.AlarmListView, error) {
	var entries []domain.AlarmListView
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("alarm-api: %s: decode entries: %w", path, err)
	}
	if entries == nil {
		return []domain.AlarmListView{}, nil
	}
	return entries, nil
}

func (c *Client) ClearRoomAlarms(ctx context.Context, roomID string) (int, error) {
	body := clearRoomReq{RoomID: roomID}
	var resp intResp
	if err := c.postJSON(ctx, contractsalarm.ClearPath, body, &resp); err != nil {
		return 0, err
	}
	return resp.Count, nil
}

// 서버에 데이터가 없으면 nil을 반환합니다.
func (c *Client) GetNextStreamInfo(ctx context.Context, channelID string) (*domain.NextStreamInfo, error) {
	var info domain.NextStreamInfo
	err := c.getJSON(ctx, contractsalarm.NextStreamPath(channelID), &info)
	if err != nil {
		if httputil.IsStatus(err, http.StatusNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if !info.Status.IsValid() {
		return nil, nil
	}
	return &info, nil
}

func (c *Client) UpdateAlarmAdvanceMinutes(ctx context.Context, minutes int) []int {
	body := updateAdvanceMinutesReq{Minutes: minutes}
	var resp minutesResp
	if ctx == nil {
		c.logger.Warn("UpdateAlarmAdvanceMinutes skipped: nil context", slog.Int("minutes", minutes))
		return []int{}
	}
	if err := c.putJSON(ctx, contractsalarm.SettingsPath, body, &resp); err != nil {
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

func (c *Client) SetRoomName(ctx context.Context, roomID, roomName string) error {
	body := setRoomNameReq{RoomID: roomID, RoomName: roomName}
	return c.putJSON(ctx, contractsalarm.RoomNamePath, body, nil)
}

func (c *Client) SetUserName(ctx context.Context, userID, userName string) error {
	body := setUserNameReq{UserID: userID, UserName: userName}
	return c.putJSON(ctx, contractsalarm.UserNamePath, body, nil)
}

func (c *Client) GetAllAlarmKeys(ctx context.Context) ([]*domain.AlarmEntry, error) {
	var entries []*domain.AlarmEntry
	if err := c.getJSON(ctx, contractsalarm.KeysPath, &entries); err != nil {
		return nil, err
	}
	if entries == nil {
		entries = []*domain.AlarmEntry{}
	}
	return entries, nil
}

func (c *Client) WarmCacheFromDB(_ context.Context) error {
	return nil
}

func (c *Client) postJSON(ctx context.Context, path string, body any, out any) error {
	return c.doJSON(ctx, http.MethodPost, path, body, out)
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) putJSON(ctx context.Context, path string, body any, out any) error {
	return c.doJSON(ctx, http.MethodPut, path, body, out)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	bodyReader, err := encodeJSONRequestBody(path, body)
	if err != nil {
		return err
	}

	resp, err := c.doRequest(ctx, method, path, bodyReader, body != nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}

	if err := httputil.DecodeJSON(resp, out); err != nil {
		return fmt.Errorf("alarm-api: %s: decode response: %w", path, err)
	}
	return nil
}

func encodeJSONRequestBody(path string, body any) (io.Reader, error) {
	if body == nil {
		return nil, nil
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return nil, fmt.Errorf("alarm-api: %s: encode request: %w", path, err)
	}
	return &buf, nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body io.Reader, hasJSONBody bool) (*http.Response, error) {
	req, err := c.newRequest(ctx, method, path, body, hasJSONBody)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("alarm-api: %s: %w", path, err)
	}
	if err := httputil.CheckStatus(resp); err != nil {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("alarm-api: %s: check status: %w", path, err)
	}
	return resp, nil
}

func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader, hasJSONBody bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("alarm-api: %s: new request: %w", path, err)
	}
	if hasJSONBody {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	return req, nil
}
