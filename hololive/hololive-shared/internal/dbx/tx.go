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
