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
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/httputil"

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
	Success bool           `json:"success"`
	Error   string         `json:"error,omitempty"`
	Message string         `json:"message,omitempty"`
	Data    jsontext.Value `json:"data,omitempty"`
}

func (c *Client) AddAlarm(ctx context.Context, req *domain.AddAlarmRequest) (bool, error) {
	if req == nil {
		return false, errors.New("alarm-api: add alarm request must not be nil")
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

	resp, err := c.postJSON[addAlarmResp](ctx, contractsalarm.AddPath, body)
	if err != nil {
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

	resp, err := c.postJSON[removeAlarmResp](ctx, contractsalarm.RemovePath, body)
	if err != nil {
		return false, err
	}

	return resp.Removed, nil
}

func (c *Client) GetRoomAlarms(ctx context.Context, roomID string) ([]string, error) {
	alarms, err := c.GetRoomAlarmsWithTypes(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("get room alarms with types: %w", err)
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
	alarms, err := c.getJSON[[]*domain.Alarm](ctx, contractsalarm.RoomAlarmsPath(roomID))
	if err != nil {
		return nil, err
	}

	if alarms == nil {
		return []*domain.Alarm{}, nil
	}

	return alarms, nil
}

func (c *Client) ListRoomAlarmsView(ctx context.Context, roomID string) ([]domain.AlarmListView, error) {
	entries, err := c.getJSON[[]domain.AlarmListView](ctx, contractsalarm.RoomAlarmsViewPath(roomID))
	if err != nil {
		return nil, err
	}

	if entries == nil {
		return []domain.AlarmListView{}, nil
	}

	return entries, nil
}

func (c *Client) ClearRoomAlarms(ctx context.Context, roomID string) (int, error) {
	body := clearRoomReq{RoomID: roomID}

	resp, err := c.postJSON[clearRoomResp](ctx, contractsalarm.ClearPath, body)
	if err != nil {
		return 0, err
	}

	return resp.Deleted, nil
}

// GetNextStreamInfo는 provider에 next-stream payload가 없으면 nil을 반환한다.
func (c *Client) GetNextStreamInfo(ctx context.Context, channelID string) (*domain.NextStreamInfo, error) {
	info, err := c.getJSON[*domain.NextStreamInfo](ctx, contractsalarm.NextStreamPath(channelID))
	if err != nil {
		return nil, err
	}

	var valid *domain.NextStreamInfo

	if info != nil && info.Status.IsValid() {
		valid = info
	}

	return valid, nil
}

func (c *Client) UpdateAlarmAdvanceMinutes(ctx context.Context, minutes int) []int {
	if ctx == nil {
		c.logger.Warn("UpdateAlarmAdvanceMinutes skipped: nil context", slog.Int("minutes", minutes))

		return []int{}
	}

	body := updateAdvanceMinutesReq{Minutes: minutes}

	resp, err := c.putJSON[minutesResp](ctx, contractsalarm.SettingsPath, body)
	if err != nil {
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
	if err := c.putNoData(ctx, contractsalarm.RoomNamePath, body); err != nil {
		return fmt.Errorf("put no data: %w", err)
	}

	return nil
}

func (c *Client) SetUserName(ctx context.Context, userID, userName string) error {
	body := setUserNameReq{UserID: userID, UserName: userName}
	if err := c.putNoData(ctx, contractsalarm.UserNamePath, body); err != nil {
		return fmt.Errorf("put no data: %w", err)
	}

	return nil
}

func (c *Client) GetAllAlarmKeys(ctx context.Context) ([]*domain.AlarmEntry, error) {
	entries, err := c.getJSON[[]*domain.AlarmEntry](ctx, contractsalarm.KeysPath)
	if err != nil {
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
