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
	"io"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestCacheInvalidateAll_WithoutValkeyStillClearsMemory(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	member := &domain.Member{Name: "miko", ChannelID: "UC_2"}
	c := &Cache{
		logger: logger,
	}
	c.byChannelID.Store(member.ChannelID, member)
	c.byName.Store(member.Name, member)
	c.allMembers.Store(allChannelIDsKey, []string{member.ChannelID})

	if err := c.InvalidateAll(ctx); err != nil {
		t.Fatalf("InvalidateAll failed: %v", err)
	}

	if _, ok := c.byChannelID.Load(member.ChannelID); ok {
		t.Fatalf("expected channel cache to be cleared")
	}
	if _, ok := c.byName.Load(member.Name); ok {
		t.Fatalf("expected name cache to be cleared")
	}
	if _, ok := c.allMembers.Load(allChannelIDsKey); ok {
		t.Fatalf("expected all-members cache to be cleared")
	}
}
