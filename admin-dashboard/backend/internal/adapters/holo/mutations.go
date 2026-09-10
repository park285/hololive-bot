package holo

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/kapu/admin-dashboard/internal/contract"
)

// StatusOnlyResponse는 upstream이 완료를 확인한 단순 변경 결과입니다.
type StatusOnlyResponse struct {
	Status  string  `json:"status"`
	Message *string `json:"message,omitempty"`
}

func (r StatusOnlyResponse) valid() bool { return r.Status == "ok" }

type mutationInput interface{ Validate() error }

func mutateOwned[In mutationInput, Out ownedResponse](ctx context.Context, client *Client, method, path string, expected int, input In) (Out, error) {
	var out Out

	if err := input.Validate(); err != nil {
		return out, fmt.Errorf("validate mutation input: %w", err)
	}

	body, err := jsonv2.Marshal(input)
	if err != nil {
		return out, contract.BadRequest("invalid request body")
	}

	if err := client.request(ctx, method, path, nil, body, expected, &out); err != nil {
		return out, err
	}

	if !out.valid() {
		return out, invalidResponse()
	}

	return out, nil
}

// AliasRequest는 기존 언어 구분과 별명 정규화 후 100자 제한을 보존합니다.
type AliasRequest struct {
	Type  string `json:"type"`
	Alias string `json:"alias"`
}

// Validate는 원문을 변경하지 않고 upstream 별명 입력 범위를 검사합니다.
func (r AliasRequest) Validate() error {
	alias := strings.Join(strings.Fields(r.Alias), " ")
	if r.Type != "ko" && r.Type != "ja" || alias == "" || utf8.RuneCountInString(alias) > 100 {
		return contract.BadRequest("invalid alias")
	}

	return nil
}

// GraduationRequest는 false와 필드 부재를 구분합니다.
type GraduationRequest struct {
	IsGraduated *bool `json:"isGraduated"`
}

// Validate는 명시한 졸업/복귀 값만 허용합니다.
func (r GraduationRequest) Validate() error {
	if r.IsGraduated == nil {
		return contract.BadRequest("isGraduated is required")
	}

	return nil
}

// ChannelRequest는 변경할 채널 ID입니다.
type ChannelRequest struct {
	ChannelID string `json:"channelId"`
}

// Validate는 빈 채널 ID를 거부합니다.
func (r ChannelRequest) Validate() error {
	if r.ChannelID == "" {
		return contract.BadRequest("channelId is required")
	}

	return nil
}

// MemberNameRequest는 변경할 멤버 이름입니다.
type MemberNameRequest struct {
	Name string `json:"name"`
}

// Validate는 빈 멤버 이름을 거부합니다.
func (r MemberNameRequest) Validate() error {
	if r.Name == "" {
		return contract.BadRequest("name is required")
	}

	return nil
}

// AddMemberRequest는 관리자 등록 화면에서 소유한 입력만 허용합니다.
type AddMemberRequest struct {
	ChannelID   string           `json:"channelId"`
	Name        string           `json:"name"`
	NameJA      *string          `json:"nameJa,omitempty"`
	NameKO      *string          `json:"nameKo,omitempty"`
	Aliases     *upstreamAliases `json:"aliases"`
	IsGraduated *bool            `json:"isGraduated"`
}

// Validate는 사용자 지정 ID·업무 외 필드를 추가하지 않고 등록의 필수 값을 검사합니다.
func (r AddMemberRequest) Validate() error {
	if r.Name == "" || r.ChannelID == "" || r.IsGraduated == nil || r.Aliases == nil || r.Aliases.KO == nil || r.Aliases.JA == nil || !present(r.Aliases.KO...) || !present(r.Aliases.JA...) {
		return contract.BadRequest("required member fields are missing")
	}

	return nil
}

func mutateMember[In mutationInput](ctx context.Context, client *Client, method, id, suffix string, input In) (StatusOnlyResponse, error) {
	path, err := memberPath(id, suffix)
	if err != nil {
		return StatusOnlyResponse{}, contract.BadRequest("invalid member id")
	}

	return mutateOwned[In, StatusOnlyResponse](ctx, client, method, path, http.StatusOK, input)
}

// AddMember는 한 번 등록하고 upstream의 201을 BFF 완료 결과로 투영합니다.
func (c *Client) AddMember(ctx context.Context, input AddMemberRequest) (StatusOnlyResponse, error) {
	return mutateOwned[AddMemberRequest, StatusOnlyResponse](ctx, c, http.MethodPost, "/api/holo/members", http.StatusCreated, input)
}

// AddAlias는 지정한 멤버에 별명을 한 번 추가합니다.
func (c *Client) AddAlias(ctx context.Context, id string, input AliasRequest) (StatusOnlyResponse, error) {
	return mutateMember(ctx, c, http.MethodPost, id, "/aliases", input)
}

// RemoveAlias는 지정한 멤버의 별명을 한 번 제거합니다.
func (c *Client) RemoveAlias(ctx context.Context, id string, input AliasRequest) (StatusOnlyResponse, error) {
	return mutateMember(ctx, c, http.MethodDelete, id, "/aliases", input)
}

// SetGraduation는 지정한 멤버의 졸업/복귀 상태를 한 번 변경합니다.
func (c *Client) SetGraduation(ctx context.Context, id string, input GraduationRequest) (StatusOnlyResponse, error) {
	return mutateMember(ctx, c, http.MethodPatch, id, "/graduation", input)
}

// UpdateChannel는 지정한 멤버의 채널 ID를 한 번 변경합니다.
func (c *Client) UpdateChannel(ctx context.Context, id string, input ChannelRequest) (StatusOnlyResponse, error) {
	return mutateMember(ctx, c, http.MethodPatch, id, "/channel", input)
}

// UpdateMemberName는 지정한 멤버의 이름을 한 번 변경합니다.
func (c *Client) UpdateMemberName(ctx context.Context, id string, input MemberNameRequest) (StatusOnlyResponse, error) {
	return mutateMember(ctx, c, http.MethodPatch, id, "/name", input)
}

// RoomRequest는 방 ACL의 추가·삭제 대상입니다.
type RoomRequest struct {
	Room string `json:"room"`
}

// Validate는 빈 방 식별자를 거부합니다.
func (r RoomRequest) Validate() error {
	if r.Room == "" {
		return contract.BadRequest("room is required")
	}

	return nil
}

// AddRoom는 방을 ACL에 한 번 추가합니다.
func (c *Client) AddRoom(ctx context.Context, input RoomRequest) (StatusOnlyResponse, error) {
	return mutateOwned[RoomRequest, StatusOnlyResponse](ctx, c, http.MethodPost, "/api/holo/rooms", http.StatusOK, input)
}

// RemoveRoom는 방을 ACL에서 한 번 제거합니다.
func (c *Client) RemoveRoom(ctx context.Context, input RoomRequest) (StatusOnlyResponse, error) {
	return mutateOwned[RoomRequest, StatusOnlyResponse](ctx, c, http.MethodDelete, "/api/holo/rooms", http.StatusOK, input)
}

// ACLRequest는 제공한 ACL 설정만 변경합니다.
type ACLRequest struct {
	Enabled *bool   `json:"enabled,omitempty"`
	Mode    *string `json:"mode,omitempty"`
}

// Validate는 현재의 mode 정규화와 하나 이상의 필수 변경값을 검사합니다.
func (r ACLRequest) Validate() error {
	if r.Enabled == nil && r.Mode == nil {
		return contract.BadRequest("enabled or mode is required")
	}

	if r.Mode != nil {
		mode := strings.ToLower(strings.TrimSpace(*r.Mode))
		if mode != "whitelist" && mode != "blacklist" {
			return contract.BadRequest("invalid ACL mode")
		}
	}

	return nil
}

// ACLResponse는 upstream이 적용했다고 확인한 설정입니다.
type ACLResponse struct {
	Status  string  `json:"status"`
	Enabled *bool   `json:"enabled"`
	Mode    *string `json:"mode"`
}

func (r ACLResponse) valid() bool {
	return r.Status == "ok" && r.Enabled != nil && r.Mode != nil && (*r.Mode == "whitelist" || *r.Mode == "blacklist")
}

// SetACL는 설정을 한 번 변경하며 upstream의 부분 실패를 완료로 만들지 않습니다.
func (c *Client) SetACL(ctx context.Context, input ACLRequest) (ACLResponse, error) {
	return mutateOwned[ACLRequest, ACLResponse](ctx, c, http.MethodPost, "/api/holo/rooms/acl", http.StatusOK, input)
}

// DeleteAlarmRequest는 방·채널의 구독 삭제 범위를 명시합니다.
type DeleteAlarmRequest struct {
	RoomID    string `json:"roomId"`
	ChannelID string `json:"channelId"`
}

// Validate는 두 식별자를 요구합니다.
func (r DeleteAlarmRequest) Validate() error {
	if r.RoomID == "" || r.ChannelID == "" {
		return contract.BadRequest("roomId and channelId are required")
	}

	return nil
}

// DeleteAlarmResponse는 실제 삭제와 이미 부재했던 경우를 구분합니다.
type DeleteAlarmResponse struct {
	Status  string `json:"status"`
	Removed *bool  `json:"removed"`
}

func (r DeleteAlarmResponse) valid() bool { return r.Status == "ok" && r.Removed != nil }

// DeleteAlarm는 해당 방·채널 구독을 한 번 제거합니다.
func (c *Client) DeleteAlarm(ctx context.Context, input DeleteAlarmRequest) (DeleteAlarmResponse, error) {
	return mutateOwned[DeleteAlarmRequest, DeleteAlarmResponse](ctx, c, http.MethodDelete, "/api/holo/alarms", http.StatusOK, input)
}

// RoomNameRequest는 방 이름 수정의 소유 필드입니다.
type RoomNameRequest struct {
	RoomID   string `json:"roomId"`
	RoomName string `json:"roomName"`
}

// Validate는 방 식별자와 이름을 요구합니다.
func (r RoomNameRequest) Validate() error {
	if r.RoomID == "" || r.RoomName == "" {
		return contract.BadRequest("roomId and roomName are required")
	}

	return nil
}

// UserNameRequest는 별도로 등록된 사용자 이름 API의 소유 입력입니다.
type UserNameRequest struct {
	UserID   string `json:"userId"`
	UserName string `json:"userName"`
}

// Validate는 사용자 식별자와 이름을 요구합니다.
func (r UserNameRequest) Validate() error {
	if r.UserID == "" || r.UserName == "" {
		return contract.BadRequest("userId and userName are required")
	}

	return nil
}

// SetRoomName는 방 표시 이름을 한 번 변경합니다.
func (c *Client) SetRoomName(ctx context.Context, input RoomNameRequest) (StatusOnlyResponse, error) {
	return mutateOwned[RoomNameRequest, StatusOnlyResponse](ctx, c, http.MethodPost, "/api/holo/names/room", http.StatusOK, input)
}

// SetUserName는 사용자 표시 이름을 한 번 변경합니다.
func (c *Client) SetUserName(ctx context.Context, input UserNameRequest) (StatusOnlyResponse, error) {
	return mutateOwned[UserNameRequest, StatusOnlyResponse](ctx, c, http.MethodPost, "/api/holo/names/user", http.StatusOK, input)
}
