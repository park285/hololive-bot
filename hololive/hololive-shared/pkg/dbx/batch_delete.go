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

package dbx

import (
	"context"
	"fmt"
	"time"
)

const DefaultBatchYield = 10 * time.Millisecond

// BatchSize는 Args 뒤 마지막 위치 파라미터로 자동 추가된다. Query의 LIMIT 자리를
// 그 마지막 번호($len(Args)+1)로 두어야 한다.
type BatchDeleteSpec struct {
	Query     string
	Args      []any
	BatchSize int
	Yield     time.Duration
}

func DeleteInBatches(ctx context.Context, q Querier, spec BatchDeleteSpec) (int64, error) {
	var total int64
	for {
		deleted, err := DeleteOneBatch(ctx, q, spec)
		total += deleted
		if err != nil {
			return total, err
		}
		if deleted < int64(spec.BatchSize) {
			return total, nil
		}
		if err := yieldBetweenBatches(ctx, spec.Yield); err != nil {
			return total, err
		}
	}
}

func DeleteOneBatch(ctx context.Context, q Querier, spec BatchDeleteSpec) (int64, error) {
	if q == nil {
		return 0, fmt.Errorf("batch delete querier is nil")
	}
	if spec.BatchSize <= 0 {
		return 0, fmt.Errorf("batch delete size must be positive: %d", spec.BatchSize)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	args := make([]any, 0, len(spec.Args)+1)
	args = append(args, spec.Args...)
	args = append(args, spec.BatchSize)

	tag, err := q.Exec(ctx, spec.Query, args...)
	if err != nil {
		return 0, fmt.Errorf("delete batch: %w", err)
	}
	return tag.RowsAffected(), nil
}

func yieldBetweenBatches(ctx context.Context, yield time.Duration) error {
	if yield <= 0 {
		yield = DefaultBatchYield
	}
	timer := time.NewTimer(yield)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
