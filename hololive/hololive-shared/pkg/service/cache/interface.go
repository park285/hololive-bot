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

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// Client defines the behavior that *Service provides.
//
// Goal: allow service consumers to depend on interfaces rather than concrete implementations.
// NOTE: keep this interface aligned with Service's public surface to avoid consumer breakage.
type Client interface {
	Get(ctx context.Context, key string, dest any) error
	MGet(ctx context.Context, keys []string) (map[string]string, error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	MSet(ctx context.Context, pairs map[string]any, ttl time.Duration) error

	Del(ctx context.Context, key string) error
	DelMany(ctx context.Context, keys []string) (int64, error)

	ScanKeys(ctx context.Context, pattern string, batchSize int64) ([]string, error)

	SAdd(ctx context.Context, key string, members []string) (int64, error)
	SRem(ctx context.Context, key string, members []string) (int64, error)
	SMembers(ctx context.Context, key string) ([]string, error)
	SIsMember(ctx context.Context, key, member string) (bool, error)

	HSet(ctx context.Context, key, field, value string) error
	HMSet(ctx context.Context, key string, fields map[string]any) error
	HGet(ctx context.Context, key, field string) (string, error)
	BatchHGet(ctx context.Context, key string, fields []string) (map[string]string, error)
	HDel(ctx context.Context, key string, fields ...string) error
	HGetAll(ctx context.Context, key string) (map[string]string, error)

	Expire(ctx context.Context, key string, ttl time.Duration) error
	Exists(ctx context.Context, key string) (bool, error)

	Close() error
	IsConnected(ctx context.Context) bool
	WaitUntilReady(ctx context.Context, timeout time.Duration) error

	GetClient() valkey.Client
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	DoMulti(ctx context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult

	Builder() valkey.Builder
	B() valkey.Builder

	CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error)
	CompareAndExpire(ctx context.Context, key, expectedValue string, ttl time.Duration) (bool, error)

	// Domain helpers (thin wrappers on top of generic operations)
	GetStreams(ctx context.Context, key string) ([]*domain.Stream, bool)
	SetStreams(ctx context.Context, key string, streams []*domain.Stream, ttl time.Duration)

	InitializeMemberDatabase(ctx context.Context, memberData map[string]string) error
	GetMemberChannelID(ctx context.Context, memberName string) (string, error)
	GetAllMembers(ctx context.Context) (map[string]string, error)
	GetMemberChannelIDWithOrg(ctx context.Context, memberName, org string) (string, error)
	GetMemberChannelIDs(ctx context.Context, memberName string) ([]string, error)
	AddMember(ctx context.Context, memberName, channelID string) error
}
