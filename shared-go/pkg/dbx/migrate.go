package dbx

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// AutoMigrate: GORM AutoMigrate 래퍼
// 컨텍스트를 전파하고 에러를 wrapping
func AutoMigrate(ctx context.Context, db *gorm.DB, models ...any) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if len(models) == 0 {
		return nil
	}
	if err := db.WithContext(ctx).AutoMigrate(models...); err != nil {
		return fmt.Errorf("auto migrate failed: %w", err)
	}
	return nil
}

// Migrator: 마이그레이션 인터페이스
// 프로젝트별 마이그레이션 로직 구현 시 사용
type Migrator interface {
	Migrate(ctx context.Context, db *gorm.DB) error
}

// RunMigrators: 여러 Migrator 순차 실행
func RunMigrators(ctx context.Context, db *gorm.DB, migrators ...Migrator) error {
	for _, m := range migrators {
		if m == nil {
			continue
		}
		if err := m.Migrate(ctx, db); err != nil {
			return fmt.Errorf("failed to run migrator: %w", err)
		}
	}
	return nil
}
