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
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (c *Service) GetStreams(ctx context.Context, key string) ([]*domain.Stream, bool) {
	var streams []*domain.Stream
	if err := c.Get(ctx, key, &streams); err != nil {
		c.logger.Debug("Cache miss or error", slog.String("key", key))
		return nil, false
	}

	if streams == nil {
		return nil, false
	}

	return streams, true
}

func (c *Service) SetStreams(ctx context.Context, key string, streams []*domain.Stream, ttl time.Duration) {
	if err := c.Set(ctx, key, streams, ttl); err != nil {
		c.logger.Error("Failed to cache streams", slog.String("key", key), slog.Any("error", err))
	}
}
