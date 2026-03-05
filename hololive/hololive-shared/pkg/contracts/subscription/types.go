// Package subscription: 구독 관련 공유 DTO 타입 정의
package subscription

// SubscribeRequest: 구독 요청 DTO (majorevent / membernews 공통)
type SubscribeRequest struct {
	RoomID   string `json:"room_id"`
	RoomName string `json:"room_name"`
}

// SubscriptionStatusResponse: 구독 상태 응답 DTO
type SubscriptionStatusResponse struct {
	Subscribed bool `json:"subscribed"`
}

// StatusResponse: 하위 호환을 위한 별칭 (deprecated)
type StatusResponse = SubscriptionStatusResponse
