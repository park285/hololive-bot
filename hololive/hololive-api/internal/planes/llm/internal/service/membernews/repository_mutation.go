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

package membernews

import (
	"context"
	"fmt"
)

func (r *Repository) Subscribe(ctx context.Context, roomID, roomName string) error {
	if r.pool == nil {
		return fmt.Errorf("membernews repository pool is nil")
	}

	query := mustSQL("repository_mutation_0033_01.sql")

	if err := r.pool.Exec(ctx, query, roomID, roomName); err != nil {
		return fmt.Errorf("subscribe member news: %w", err)
	}

	r.writeThroughSubscribe(ctx, roomID, roomName)
	return nil
}

func (r *Repository) Unsubscribe(ctx context.Context, roomID string) error {
	if r.pool == nil {
		return fmt.Errorf("membernews repository pool is nil")
	}

	query := mustSQL("repository_mutation_0054_02.sql")
	if err := r.pool.Exec(ctx, query, roomID); err != nil {
		return fmt.Errorf("unsubscribe member news: %w", err)
	}

	r.writeThroughUnsubscribe(ctx, roomID)
	return nil
}
