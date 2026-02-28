package dbx

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// InTx: 트랜잭션 내에서 fn 실행
// 수동 Begin/Commit 대신 이 헬퍼 사용 권장
func InTx(ctx context.Context, db *gorm.DB, fn func(tx *gorm.DB) error) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if fn == nil {
		return nil
	}
	if err := db.WithContext(ctx).Transaction(fn); err != nil {
		return fmt.Errorf("transaction failed: %w", err)
	}
	return nil
}

// InTxWithResult: 트랜잭션 내에서 fn 실행하고 결과 반환
func InTxWithResult[T any](ctx context.Context, db *gorm.DB, fn func(tx *gorm.DB) (T, error)) (T, error) {
	var result T
	if db == nil {
		return result, fmt.Errorf("db is nil")
	}
	if fn == nil {
		return result, nil
	}

	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var txErr error
		result, txErr = fn(tx)
		return txErr
	})
	if err != nil {
		return result, fmt.Errorf("transaction failed: %w", err)
	}
	return result, nil
}
