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

package acl

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/valkey-io/valkey-go"
)

const (
	aclRoomsTempKeySeparator = ":tmp:"
)

var aclRoomsTempKeySeq atomic.Uint64

const renameRoomsKeyScript = `
local source = ARGV[1]
local target = ARGV[2]

if redis.call('EXISTS', source) == 1 then
	redis.call('RENAME', source, target)
else
	redis.call('DEL', target)
end

return 1
`

func (s *Service) syncRoomsToValkeyAtomic(ctx context.Context, key string, rooms []string) error {
	if len(rooms) == 0 {
		if err := s.cache.Del(ctx, key); err != nil {
			return fmt.Errorf("clear %s: %w", key, err)
		}

		return nil
	}

	tempKey := fmt.Sprintf("%s%s%d", key, aclRoomsTempKeySeparator, aclRoomsTempKeySeq.Add(1))

	if err := s.cache.Del(ctx, tempKey); err != nil {
		return fmt.Errorf("cleanup temp %s: %w", tempKey, err)
	}

	if _, err := s.cache.SAdd(ctx, tempKey, rooms); err != nil {
		return fmt.Errorf("populate temp %s: %w", tempKey, err)
	}

	if err := s.renameRoomsKey(ctx, tempKey, key, rooms); err != nil {
		if cleanupErr := s.cache.Del(context.WithoutCancel(ctx), tempKey); cleanupErr != nil {
			return fmt.Errorf("swap %s from %s: %w (cleanup temp: %w)", key, tempKey, err, cleanupErr)
		}

		return fmt.Errorf("swap %s from %s: %w", key, tempKey, err)
	}

	return nil
}

func (s *Service) renameRoomsKey(ctx context.Context, tempKey, key string, rooms []string) error {
	if s.renameRoomsKeyFunc != nil {
		if err := s.renameRoomsKeyFunc(ctx, tempKey, key, rooms); err != nil {
			return fmt.Errorf("custom rename %s from %s: %w", key, tempKey, err)
		}

		return nil
	}

	client, builder, ok := s.rawCacheEvalClient()
	if !ok {
		if err := s.cache.Del(ctx, key); err != nil {
			return fmt.Errorf("fallback clear %s: %w", key, err)
		}

		if len(rooms) == 0 {
			return nil
		}

		if _, err := s.cache.SAdd(ctx, key, rooms); err != nil {
			return fmt.Errorf("fallback write %s: %w", key, err)
		}

		return nil
	}

	resp := client.Do(ctx, builder.Eval().
		Script(renameRoomsKeyScript).
		Numkeys(0).
		Arg(tempKey, key).
		Build(),
	)
	if err := resp.Error(); err != nil {
		return fmt.Errorf("eval rename %s from %s: %w", key, tempKey, err)
	}

	return nil
}

func (s *Service) rawCacheEvalClient() (_ valkey.Client, _ valkey.Builder, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	client := s.cache.GetClient()
	builder := s.cache.B()

	if client == nil {
		return nil, valkey.Builder{}, false
	}

	return client, builder, true
}
