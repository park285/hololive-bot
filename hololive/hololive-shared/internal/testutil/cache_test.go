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

package testutil

import (
	"context"
	"testing"
	"time"
)

type cachePayload struct {
	Value string `json:"value"`
}

func TestNewTestCacheServiceWithMini(t *testing.T) {
	ctx := context.Background()
	service, mini := NewTestCacheServiceWithMini(t, ctx)

	if service == nil {
		t.Fatal("service is nil")
	}
	if mini == nil {
		t.Fatal("miniredis is nil")
	}

	in := cachePayload{Value: "ok"}
	if err := service.Set(ctx, "test:key", in, time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}

	var out cachePayload
	if err := service.Get(ctx, "test:key", &out); err != nil {
		t.Fatalf("get: %v", err)
	}
	if out.Value != in.Value {
		t.Fatalf("value mismatch: got %q want %q", out.Value, in.Value)
	}
}

func TestNewTestCacheService(t *testing.T) {
	ctx := context.Background()
	service := NewTestCacheService(t, ctx)
	if service == nil {
		t.Fatal("service is nil")
	}
}
