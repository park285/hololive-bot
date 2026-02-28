// Package flagx: 엔티티 플래그 관리를 위한 유틸리티 패키지입니다.
package flagx

import (
	"context"
	"time"
)

// FlagRecord: DB에 저장된 플래그 레코드입니다.
type FlagRecord struct {
	EntityID  string    // 플래그가 부착된 엔티티 ID
	Flag      Flag      // 플래그 값
	CreatedAt time.Time // 생성 시각
	CreatedBy string    // 생성자 (trace/correlation ID)
}

// Repository: 플래그 저장소 인터페이스입니다.
// 각 엔티티 타입별로 별도의 테이블을 사용합니다.
type Repository interface {
	// Set: 엔티티에 플래그를 설정합니다.
	// 이미 존재하면 createdBy만 업데이트합니다 (idempotent).
	Set(ctx context.Context, entityID string, flag Flag, createdBy string) error

	// Unset: 엔티티에서 플래그를 제거합니다.
	// 존재하지 않아도 에러를 반환하지 않습니다 (idempotent).
	Unset(ctx context.Context, entityID string, flag Flag) error

	// Has: 엔티티에 플래그가 존재하는지 확인합니다.
	Has(ctx context.Context, entityID string, flag Flag) (bool, error)

	// List: 엔티티의 모든 플래그 레코드를 조회합니다.
	List(ctx context.Context, entityID string) ([]FlagRecord, error)

	// ListByFlag: 특정 플래그를 가진 모든 엔티티를 조회합니다.
	ListByFlag(ctx context.Context, flag Flag) ([]FlagRecord, error)
}
