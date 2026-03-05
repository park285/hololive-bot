// Package subscription: 구독 저장소 공통 인터페이스
package subscription

import "context"

// SubscriptionRepository: 방 구독 관리 공통 인터페이스 (majorevent, membernews 구현)
// T는 ListSubscribedRooms에서 반환하는 구독 방 DTO 타입입니다.
type SubscriptionRepository[T any] interface {
	Subscribe(ctx context.Context, roomID, roomName string) error
	IsSubscribed(ctx context.Context, roomID string) (bool, error)
	ListSubscribedRooms(ctx context.Context) ([]T, error)
}
