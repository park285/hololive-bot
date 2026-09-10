package holo

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kapu/admin-dashboard/internal/contract"
	"github.com/kapu/hololive-shared/pkg/constants"
)

type ownedResponse interface{ valid() bool }

func getOwned[T any, P interface {
	*T
	ownedResponse
}](ctx context.Context, client *Client, path string, query url.Values) (T, error) {
	var out T

	if err := client.request(ctx, http.MethodGet, path, query, nil, http.StatusOK, &out); err != nil {
		return out, err
	}

	if !P(&out).valid() {
		return out, invalidResponse()
	}

	return out, nil
}

func present[T any](values ...*T) bool {
	for _, value := range values {
		if value == nil {
			return false
		}
	}

	return true
}

func nonnegative(values ...*int64) bool {
	for _, value := range values {
		if value == nil || *value < 0 {
			return false
		}
	}

	return true
}

func timestamp(value *string) bool {
	if value == nil {
		return true
	}

	_, err := time.Parse(time.RFC3339, *value)

	return err == nil
}

// GetAlarms는 고정 upstream에서 소유한 알람 필드만 조회합니다.
func (c *Client) GetAlarms(ctx context.Context) (AlarmsResponse, error) {
	return getOwned[AlarmsResponse](ctx, c, "/api/holo/alarms", nil)
}

// RoomsResponse는 방 ACL 설정과 정확한 방 식별자 목록입니다.
type RoomsResponse struct {
	Status     string    `json:"status"`
	Rooms      []*string `json:"rooms"`
	ACLEnabled *bool     `json:"aclEnabled"`
	ACLMode    *string   `json:"aclMode"`
}

func (r RoomsResponse) valid() bool {
	return r.Status == "ok" && r.Rooms != nil && r.ACLEnabled != nil && r.ACLMode != nil && (*r.ACLMode == "whitelist" || *r.ACLMode == "blacklist") && present(r.Rooms...)
}

// GetRooms는 관리자 방 ACL을 조회합니다.
func (c *Client) GetRooms(ctx context.Context) (RoomsResponse, error) {
	return getOwned[RoomsResponse](ctx, c, "/api/holo/rooms", nil)
}

// JoinedRoom은 upstream이 Iris에서 확인한 방의 표시 필드입니다.
type JoinedRoom struct {
	ChatID      *string `json:"chatId"`
	Name        *string `json:"name"`
	Type        *string `json:"type"`
	MemberCount *int64  `json:"memberCount"`
}

// JoinedRoomsResponse는 확인한 참여 방 목록입니다.
type JoinedRoomsResponse struct {
	Status string       `json:"status"`
	Rooms  []JoinedRoom `json:"rooms"`
}

func (r JoinedRoomsResponse) valid() bool {
	if r.Status != "ok" || r.Rooms == nil {
		return false
	}

	for _, room := range r.Rooms {
		if !present(room.ChatID, room.Name, room.Type) || !nonnegative(room.MemberCount) {
			return false
		}
	}

	return true
}

// GetJoinedRooms는 upstream을 통해 참여 방을 조회하며 Iris를 직접 호출하지 않습니다.
func (c *Client) GetJoinedRooms(ctx context.Context) (JoinedRoomsResponse, error) {
	return getOwned[JoinedRoomsResponse](ctx, c, "/api/holo/rooms/joined", nil)
}

// Settings는 관리자 화면이 소유한 알람 설정입니다.
type Settings struct {
	AlarmAdvanceMinutes *int `json:"alarmAdvanceMinutes"`
}

func (s Settings) valid() bool {
	return s.AlarmAdvanceMinutes != nil && *s.AlarmAdvanceMinutes >= 0 && *s.AlarmAdvanceMinutes <= 1440
}

// SettingsResponse는 0~1440 범위의 저장된 설정을 보존합니다.
type SettingsResponse struct {
	Status   string    `json:"status"`
	Settings *Settings `json:"settings"`
}

func (r SettingsResponse) valid() bool {
	return r.Status == "ok" && r.Settings != nil && r.Settings.valid()
}

// GetSettings는 다른 upstream 설정이나 runtime 필드를 노출하지 않습니다.
func (c *Client) GetSettings(ctx context.Context) (SettingsResponse, error) {
	return getOwned[SettingsResponse](ctx, c, "/api/holo/settings", nil)
}

// StatsResponse는 관리자 업무 통계이며 누락된 count를 0으로 바꾸지 않습니다.
type StatsResponse struct {
	Status  string  `json:"status"`
	Members *int64  `json:"members"`
	Alarms  *int64  `json:"alarms"`
	Rooms   *int64  `json:"rooms"`
	Version *string `json:"version"`
	Uptime  *string `json:"uptime"`
}

func (r StatsResponse) valid() bool {
	return r.Status == "ok" && nonnegative(r.Members, r.Alarms, r.Rooms) && present(r.Version, r.Uptime)
}

// GetStats는 고정 upstream의 업무 통계를 조회합니다.
func (c *Client) GetStats(ctx context.Context) (StatsResponse, error) {
	return getOwned[StatsResponse](ctx, c, "/api/holo/stats", nil)
}

// Stream은 관리자 방송 화면이 사용하는 필드만 포함합니다.
type Stream struct {
	ID             *string `json:"id"`
	Title          *string `json:"title"`
	Status         *string `json:"status"`
	ChannelID      *string `json:"channel_id"`
	ChannelName    *string `json:"channel_name,omitempty"`
	Link           *string `json:"link,omitempty"`
	Thumbnail      *string `json:"thumbnail,omitempty"`
	StartActual    *string `json:"start_actual,omitempty"`
	StartScheduled *string `json:"start_scheduled,omitempty"`
}

// StreamsResponse는 확인한 방송 목록과 조직 구분입니다.
type StreamsResponse struct {
	Status  string   `json:"status"`
	Streams []Stream `json:"streams"`
	Org     *string  `json:"org,omitempty"`
}

func (r StreamsResponse) valid() bool {
	if r.Status != "ok" || r.Streams == nil {
		return false
	}

	for _, stream := range r.Streams {
		if !present(stream.ID, stream.Title, stream.Status, stream.ChannelID) || !timestamp(stream.StartActual) || !timestamp(stream.StartScheduled) {
			return false
		}
	}

	return true
}

// GetLiveStreams는 요청한 조직의 진행 중 방송을 조회합니다.
func (c *Client) GetLiveStreams(ctx context.Context, org *string) (StreamsResponse, error) {
	query, err := streamQuery(org)
	if err != nil {
		return StreamsResponse{}, err
	}

	return getOwned[StreamsResponse](ctx, c, "/api/holo/streams/live", query)
}

// GetUpcomingStreams는 요청한 조직의 예정 방송을 조회합니다.
func (c *Client) GetUpcomingStreams(ctx context.Context, org *string) (StreamsResponse, error) {
	query, err := streamQuery(org)
	if err != nil {
		return StreamsResponse{}, err
	}

	return getOwned[StreamsResponse](ctx, c, "/api/holo/streams/upcoming", query)
}

func streamQuery(org *string) (url.Values, error) {
	query := make(url.Values)

	if org == nil {
		return query, nil
	}

	normalized := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(*org)), "!")
	if normalized == "" {
		return nil, contract.BadRequest("org must not be empty")
	}

	// provider/service_streams.go의 기존 holo/indie alias와 정규화 계약을 보존합니다.
	allowed := []string{"holo", "indie", constants.HolodexAPIParams.OrgHololive, constants.HolodexAPIParams.OrgVSpo, constants.HolodexAPIParams.OrgStellive, constants.HolodexAPIParams.OrgIndie, constants.HolodexAPIParams.OrgAll}
	for _, candidate := range allowed {
		if strings.EqualFold(normalized, candidate) {
			query.Set("org", *org)

			return query, nil
		}
	}

	return nil, contract.BadRequest("unsupported org")
}
