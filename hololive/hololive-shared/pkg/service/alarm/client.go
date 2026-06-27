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

	"github.com/park285/shared-go/pkg/httputil"
	json "github.com/park285/shared-go/pkg/json"

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

type addAlarmResp struct {
	Added bool `json:"added"`
}

type removeAlarmResp struct {
	Removed bool `json:"removed"`
}

type clearRoomResp struct {
	Deleted int `json:"deleted"`
}

type minutesResp struct {
	TargetMinutes []int `json:"target_minutes"`
}

type apiEnvelope struct {
	Success bool            `json:"success"`
	Error   string          `json:"error,omitempty"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (c *Client) AddAlarm(ctx context.Context, req *domain.AddAlarmRequest) (bool, error) {
	if req == nil {
		return false, fmt.Errorf("alarm-api: add alarm request must not be nil")
	}
	body := addAlarmReq{
		RoomID:     req.RoomID,
		UserID:     req.UserID,
		ChannelID:  req.ChannelID,
		MemberName: req.MemberName,
		RoomName:   req.RoomName,
		UserName:   req.UserName,
		AlarmTypes: req.AlarmTypes,
	}
	var resp addAlarmResp
	if err := c.postJSON(ctx, contractsalarm.AddPath, body, &resp); err != nil {
		return false, err
	}
	return resp.Added, nil
}

func (c *Client) RemoveAlarm(ctx context.Context, roomID, channelID string, alarmTypes domain.AlarmTypes) (bool, error) {
	body := removeAlarmReq{
		RoomID:     roomID,
		ChannelID:  channelID,
		AlarmTypes: alarmTypes,
	}
	var resp removeAlarmResp
	if err := c.postJSON(ctx, contractsalarm.RemovePath, body, &resp); err != nil {
		return false, err
	}
	return resp.Removed, nil
}

func (c *Client) GetRoomAlarms(ctx context.Context, roomID string) ([]string, error) {
	alarms, err := c.GetRoomAlarmsWithTypes(ctx, roomID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(alarms))
	for _, alarm := range alarms {
		if alarm != nil {
			ids = append(ids, alarm.ChannelID)
		}
	}
	return ids, nil
}

func (c *Client) GetRoomAlarmsWithTypes(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	var alarms []*domain.Alarm
	if err := c.getJSON(ctx, contractsalarm.RoomAlarmsPath(roomID), &alarms); err != nil {
		return nil, err
	}
	if alarms == nil {
		return []*domain.Alarm{}, nil
	}
	return alarms, nil
}

func (c *Client) ListRoomAlarmsView(ctx context.Context, roomID string) ([]domain.AlarmListView, error) {
	var entries []domain.AlarmListView
	if err := c.getJSON(ctx, contractsalarm.RoomAlarmsViewPath(roomID), &entries); err != nil {
		return nil, err
	}
	if entries == nil {
		return []domain.AlarmListView{}, nil
	}
	return entries, nil
}

func (c *Client) ClearRoomAlarms(ctx context.Context, roomID string) (int, error) {
	body := clearRoomReq{RoomID: roomID}
	var resp clearRoomResp
	if err := c.postJSON(ctx, contractsalarm.ClearPath, body, &resp); err != nil {
		return 0, err
	}
	return resp.Deleted, nil
}

// GetNextStreamInfo returns nil when the provider has no next-stream payload.
func (c *Client) GetNextStreamInfo(ctx context.Context, channelID string) (*domain.NextStreamInfo, error) {
	var info *domain.NextStreamInfo
	if err := c.getJSON(ctx, contractsalarm.NextStreamPath(channelID), &info); err != nil {
		return nil, err
	}
	if info == nil || !info.Status.IsValid() {
		return nil, nil
	}
	return info, nil
}

func (c *Client) UpdateAlarmAdvanceMinutes(ctx context.Context, minutes int) []int {
	if ctx == nil {
		c.logger.Warn("UpdateAlarmAdvanceMinutes skipped: nil context", slog.Int("minutes", minutes))
		return []int{}
	}
	body := updateAdvanceMinutesReq{Minutes: minutes}
	var resp minutesResp
	if err := c.putJSON(ctx, contractsalarm.SettingsPath, body, &resp); err != nil {
		c.logger.Warn("UpdateAlarmAdvanceMinutes failed",
			slog.Int("minutes", minutes),
			slog.Any("error", err),
		)
		return []int{}
	}
	result := append([]int(nil), resp.TargetMinutes...)
	c.targetMinutesMu.Lock()
	c.targetMinutes = result
	c.targetMinutesMu.Unlock()
	return append([]int(nil), result...)
}

func (c *Client) GetTargetMinutes() []int {
	c.targetMinutesMu.RLock()
	defer c.targetMinutesMu.RUnlock()
	return append([]int(nil), c.targetMinutes...)
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
		return []*domain.AlarmEntry{}, nil
	}
	return entries, nil
}

func (c *Client) WarmCacheFromDB(_ context.Context) error {
	return nil
}

func (c *Client) postJSON(ctx context.Context, path string, body, out any) error {
	return c.doJSON(ctx, http.MethodPost, path, body, out)
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	return c.doJSON(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) putJSON(ctx context.Context, path string, body, out any) error {
	return c.doJSON(ctx, http.MethodPut, path, body, out)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	bodyReader, err := encodeJSONRequestBody(path, body)
	if err != nil {
		return err
	}

	resp, err := c.doRequest(ctx, method, path, bodyReader, body != nil)
	if err != nil {
		return err
	}
	defer c.closeResponseBody(resp, path)

	if err := decodeAPIEnvelope(path, resp.Body, out); err != nil {
		return err
	}
	return nil
}

func decodeAPIEnvelope(path string, body io.Reader, out any) error {
	var envelope apiEnvelope
	if err := json.NewDecoder(body).Decode(&envelope); err != nil {
		return fmt.Errorf("alarm-api: %s: decode envelope: %w", path, err)
	}
	if !envelope.Success {
		return fmt.Errorf("alarm-api: %s: %s", path, envelopeFailureMessage(envelope))
	}
	if !envelopeHasData(out, envelope.Data) {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("alarm-api: %s: decode data: %w", path, err)
	}
	return nil
}

func envelopeFailureMessage(envelope apiEnvelope) string {
	message := strings.TrimSpace(envelope.Message)
	if message == "" {
		message = strings.TrimSpace(envelope.Error)
	}
	if message == "" {
		message = "provider returned unsuccessful response"
	}
	return message
}

func envelopeHasData(out any, data json.RawMessage) bool {
	if out == nil || len(data) == 0 {
		return false
	}
	return !bytes.Equal(bytes.TrimSpace(data), []byte("null"))
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
		if resp == nil {
			err = fmt.Errorf("nil response: %w", err)
		}
		return nil, fmt.Errorf("alarm-api: %s: %w", path, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("alarm-api: %s: nil response", path)
	}
	if err := c.validateResponse(path, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) validateResponse(path string, resp *http.Response) error {
	if resp == nil {
		return fmt.Errorf("alarm-api: %s: nil response", path)
	}
	if resp.Body == nil {
		return fmt.Errorf("alarm-api: %s: nil response body", path)
	}
	if err := httputil.CheckStatus(resp); err != nil {
		return fmt.Errorf("alarm-api: %s: check status: %w", path, err)
	}
	return nil
}

func (c *Client) closeResponseBody(resp *http.Response, path string) {
	if resp == nil || resp.Body == nil {
		return
	}
	if err := resp.Body.Close(); err != nil {
		c.logger.Warn("Failed to close alarm API response body", slog.String("path", path), slog.Any("error", err))
	}
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
