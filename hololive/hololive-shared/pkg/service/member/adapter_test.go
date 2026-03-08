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
)

func testAdapterLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewMemberServiceAdapter_DetachesCancellation(t *testing.T) {
	parent := context.WithValue(context.Background(), struct{}{}, "value")
	ctx, cancel := context.WithCancel(parent)
	cancel()

	adapter := NewMemberServiceAdapter(ctx, nil, testAdapterLogger())
	if adapter == nil {
		t.Fatal("adapter is nil")
	}
	if err := adapter.ctx.Err(); err != nil {
		t.Fatalf("adapter ctx should not inherit cancellation, got err=%v", err)
	}
	if got := adapter.ctx.Value(struct{}{}); got != "value" {
		t.Fatalf("adapter ctx should preserve values, got=%v", got)
	}
}

func TestServiceAdapter_WithContext_UsesProvidedContext(t *testing.T) {
	adapter := NewMemberServiceAdapter(context.Background(), nil, testAdapterLogger())

	child, cancel := context.WithCancel(context.Background())
	cancel()

	derived, ok := adapter.WithContext(child).(*ServiceAdapter)
	if !ok {
		t.Fatal("WithContext should return *ServiceAdapter")
	}
	if err := derived.ctx.Err(); err != context.Canceled {
		t.Fatalf("derived ctx should preserve provided cancellation, got=%v", err)
	}
}
