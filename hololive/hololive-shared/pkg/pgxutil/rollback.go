// Copyright (c) 2026 Kapu
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

// Package pgxutil provides runtime-neutral pgx transaction lifecycle helpers.
package pgxutil

import (
	"context"
	"fmt"
	"time"
)

const rollbackTimeout = 5 * time.Second

// Rollbacker는 정리에 필요한 pgx 트랜잭션 기능이다.
type Rollbacker interface {
	Rollback(ctx context.Context) error
}

// Rollback은 컨텍스트 값을 유지하되 요청 취소와 분리해 실행한다.
// 연결 장애가 정리를 무한히 막지 않도록 제한 시간을 둔다.
func Rollback(ctx context.Context, tx Rollbacker) error {
	if tx == nil {
		return nil
	}

	base := context.WithoutCancel(ctx)
	rollbackCtx, cancel := context.WithTimeout(base, rollbackTimeout)

	defer cancel()

	if err := tx.Rollback(rollbackCtx); err != nil {
		return fmt.Errorf("rollback: %w", err)
	}

	return nil
}
