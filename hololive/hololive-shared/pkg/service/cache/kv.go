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

package cache

import (
	"context"
	"time"
)

type KeyValueCache interface {
	Get(ctx context.Context, key string, dest any) error
	GetString(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	MSet(ctx context.Context, pairs map[string]any, ttl time.Duration) error
	Del(ctx context.Context, key string) error
	DelMany(ctx context.Context, keys []string) (int64, error)
	ScanKeys(ctx context.Context, pattern string, batchSize int64) ([]string, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
	Exists(ctx context.Context, key string) (bool, error)
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	SetNXMulti(ctx context.Context, entries []SetNXEntry) ([]SetNXResult, error)
}
