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

package member

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

// WarmUpCache는 전체 멤버 snapshot을 적재해 채널·이름·별칭 point 조회가 PostgreSQL 없이 응답하도록 한다.
func (c *Cache) WarmUpCache(ctx context.Context) error {
	if c == nil {
		return errors.New("member cache is nil")
	}

	_, generation := c.allMembersView()

	snap, err := c.loadAllMembersSnapshot(ctx, generation)
	if err != nil {
		return fmt.Errorf("failed to load all members: %w", err)
	}

	if c.logger != nil {
		c.logger.Info("Member cache warmed up", slog.Int("total_members", len(snap.members)))
	}

	return nil
}
