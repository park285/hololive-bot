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
