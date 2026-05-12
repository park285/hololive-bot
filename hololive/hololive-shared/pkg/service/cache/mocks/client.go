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

package mocks

import (
	"context"
	"errors"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// Client is a manual mock for cache.Client.
//
// NOTE: cache.Client is intentionally broad (matches *cache.Service public surface).
// For unit tests, zero-value Client is strict by default; unconfigured calls panic
// unless Lenient is explicitly enabled.
type Client struct {
	Lenient bool

	GetFunc      func(ctx context.Context, key string, dest any) error
	MGetFunc     func(ctx context.Context, keys []string) (map[string]string, error)
	SetFunc      func(ctx context.Context, key string, value any, ttl time.Duration) error
	MSetFunc     func(ctx context.Context, pairs map[string]any, ttl time.Duration) error
	DelFunc      func(ctx context.Context, key string) error
	DelManyFunc  func(ctx context.Context, keys []string) (int64, error)
	ScanKeysFunc func(ctx context.Context, pattern string, batchSize int64) ([]string, error)

	SAddFunc      func(ctx context.Context, key string, members []string) (int64, error)
	SRemFunc      func(ctx context.Context, key string, members []string) (int64, error)
	SMembersFunc  func(ctx context.Context, key string) ([]string, error)
	SIsMemberFunc func(ctx context.Context, key, member string) (bool, error)

	HSetFunc      func(ctx context.Context, key, field, value string) error
	HMSetFunc     func(ctx context.Context, key string, fields map[string]any) error
	HGetFunc      func(ctx context.Context, key, field string) (string, error)
	BatchHGetFunc func(ctx context.Context, key string, fields []string) (map[string]string, error)
	HDelFunc      func(ctx context.Context, key string, fields ...string) error
	HGetAllFunc   func(ctx context.Context, key string) (map[string]string, error)

	ExpireFunc func(ctx context.Context, key string, ttl time.Duration) error
	ExistsFunc func(ctx context.Context, key string) (bool, error)

	CloseFunc          func() error
	IsConnectedFunc    func(ctx context.Context) bool
	WaitUntilReadyFunc func(ctx context.Context, timeout time.Duration) error

	GetClientFunc func() valkey.Client
	SetNXFunc     func(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	DoMultiFunc   func(ctx context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult
	BuilderFunc   func() valkey.Builder
	BFunc         func() valkey.Builder

	CompareAndDeleteFunc func(ctx context.Context, key, expectedValue string) (bool, error)
	CompareAndExpireFunc func(ctx context.Context, key, expectedValue string, ttl time.Duration) (bool, error)

	GetStreamsFunc func(ctx context.Context, key string) ([]*domain.Stream, bool)
	SetStreamsFunc func(ctx context.Context, key string, streams []*domain.Stream, ttl time.Duration)

	InitializeMemberDatabaseFunc  func(ctx context.Context, memberData map[string]string) error
	GetMemberChannelIDFunc        func(ctx context.Context, memberName string) (string, error)
	GetAllMembersFunc             func(ctx context.Context) (map[string]string, error)
	GetMemberChannelIDWithOrgFunc func(ctx context.Context, memberName, org string) (string, error)
	GetMemberChannelIDsFunc       func(ctx context.Context, memberName string) ([]string, error)
	AddMemberFunc                 func(ctx context.Context, memberName, channelID string) error
}

var _ cache.Client = (*Client)(nil)

var ErrUnimplemented = errors.New("cache mock: method not configured")

func NewStrictClient() *Client {
	return &Client{}
}

func NewLenientClient() *Client {
	return &Client{Lenient: true}
}

func (m *Client) panicIfUnset(name string) {
	if m == nil || !m.Lenient {
		panic("cache mock: " + name + " not set")
	}
}

func (m *Client) unsetError(name string) error {
	m.panicIfUnset(name)
	return nil
}

func (m *Client) unsetInt64(name string) (int64, error) {
	m.panicIfUnset(name)
	return 0, nil
}

func (m *Client) Get(ctx context.Context, key string, dest any) error {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, key, dest)
	}
	m.panicIfUnset("GetFunc")
	return nil
}

func (m *Client) MGet(ctx context.Context, keys []string) (map[string]string, error) {
	if m.MGetFunc != nil {
		return m.MGetFunc(ctx, keys)
	}
	m.panicIfUnset("MGetFunc")
	return nil, nil
}

func (m *Client) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if m.SetFunc != nil {
		return m.SetFunc(ctx, key, value, ttl)
	}
	return m.unsetError("SetFunc")
}

func (m *Client) MSet(ctx context.Context, pairs map[string]any, ttl time.Duration) error {
	if m.MSetFunc != nil {
		return m.MSetFunc(ctx, pairs, ttl)
	}
	return m.unsetError("MSetFunc")
}

func (m *Client) Del(ctx context.Context, key string) error {
	if m.DelFunc != nil {
		return m.DelFunc(ctx, key)
	}
	return m.unsetError("DelFunc")
}

func (m *Client) DelMany(ctx context.Context, keys []string) (int64, error) {
	if m.DelManyFunc != nil {
		return m.DelManyFunc(ctx, keys)
	}
	return m.unsetInt64("DelManyFunc")
}

func (m *Client) ScanKeys(ctx context.Context, pattern string, batchSize int64) ([]string, error) {
	if m.ScanKeysFunc != nil {
		return m.ScanKeysFunc(ctx, pattern, batchSize)
	}
	m.panicIfUnset("ScanKeysFunc")
	return nil, nil
}

func (m *Client) SAdd(ctx context.Context, key string, members []string) (int64, error) {
	if m.SAddFunc != nil {
		return m.SAddFunc(ctx, key, members)
	}
	return m.unsetInt64("SAddFunc")
}

func (m *Client) SRem(ctx context.Context, key string, members []string) (int64, error) {
	if m.SRemFunc != nil {
		return m.SRemFunc(ctx, key, members)
	}
	return m.unsetInt64("SRemFunc")
}

func (m *Client) SMembers(ctx context.Context, key string) ([]string, error) {
	if m.SMembersFunc != nil {
		return m.SMembersFunc(ctx, key)
	}
	m.panicIfUnset("SMembersFunc")
	return nil, nil
}

func (m *Client) SIsMember(ctx context.Context, key, member string) (bool, error) {
	if m.SIsMemberFunc != nil {
		return m.SIsMemberFunc(ctx, key, member)
	}
	m.panicIfUnset("SIsMemberFunc")
	return false, nil
}

func (m *Client) HSet(ctx context.Context, key, field, value string) error {
	if m.HSetFunc != nil {
		return m.HSetFunc(ctx, key, field, value)
	}
	return m.unsetError("HSetFunc")
}

func (m *Client) HMSet(ctx context.Context, key string, fields map[string]any) error {
	if m.HMSetFunc != nil {
		return m.HMSetFunc(ctx, key, fields)
	}
	return m.unsetError("HMSetFunc")
}

func (m *Client) HGet(ctx context.Context, key, field string) (string, error) {
	if m.HGetFunc != nil {
		return m.HGetFunc(ctx, key, field)
	}
	m.panicIfUnset("HGetFunc")
	return "", nil
}

func (m *Client) BatchHGet(ctx context.Context, key string, fields []string) (map[string]string, error) {
	if m.BatchHGetFunc != nil {
		return m.BatchHGetFunc(ctx, key, fields)
	}
	m.panicIfUnset("BatchHGetFunc")
	return nil, nil
}

func (m *Client) HDel(ctx context.Context, key string, fields ...string) error {
	if m.HDelFunc != nil {
		return m.HDelFunc(ctx, key, fields...)
	}
	return m.unsetError("HDelFunc")
}

func (m *Client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	if m.HGetAllFunc != nil {
		return m.HGetAllFunc(ctx, key)
	}
	m.panicIfUnset("HGetAllFunc")
	return nil, nil
}

func (m *Client) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if m.ExpireFunc != nil {
		return m.ExpireFunc(ctx, key, ttl)
	}
	return m.unsetError("ExpireFunc")
}

func (m *Client) Exists(ctx context.Context, key string) (bool, error) {
	if m.ExistsFunc != nil {
		return m.ExistsFunc(ctx, key)
	}
	m.panicIfUnset("ExistsFunc")
	return false, nil
}

func (m *Client) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	m.panicIfUnset("CloseFunc")
	return nil
}

func (m *Client) IsConnected(ctx context.Context) bool {
	if m.IsConnectedFunc != nil {
		return m.IsConnectedFunc(ctx)
	}
	m.panicIfUnset("IsConnectedFunc")
	return false
}

func (m *Client) WaitUntilReady(ctx context.Context, timeout time.Duration) error {
	if m.WaitUntilReadyFunc != nil {
		return m.WaitUntilReadyFunc(ctx, timeout)
	}
	m.panicIfUnset("WaitUntilReadyFunc")
	return nil
}

func (m *Client) GetClient() valkey.Client {
	if m.GetClientFunc != nil {
		return m.GetClientFunc()
	}
	m.panicIfUnset("GetClientFunc")
	return nil
}

func (m *Client) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if m.SetNXFunc != nil {
		return m.SetNXFunc(ctx, key, value, ttl)
	}
	m.panicIfUnset("SetNXFunc")
	return false, nil
}

func (m *Client) DoMulti(ctx context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult {
	if m.DoMultiFunc != nil {
		return m.DoMultiFunc(ctx, cmds...)
	}
	m.panicIfUnset("DoMultiFunc")
	return nil
}

func (m *Client) Builder() valkey.Builder {
	if m.BuilderFunc != nil {
		return m.BuilderFunc()
	}
	m.panicIfUnset("BuilderFunc")
	return valkey.Builder{}
}

func (m *Client) B() valkey.Builder {
	if m.BFunc != nil {
		return m.BFunc()
	}
	m.panicIfUnset("BFunc")
	return valkey.Builder{}
}

func (m *Client) CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error) {
	if m.CompareAndDeleteFunc != nil {
		return m.CompareAndDeleteFunc(ctx, key, expectedValue)
	}
	m.panicIfUnset("CompareAndDeleteFunc")
	return false, nil
}

func (m *Client) CompareAndExpire(ctx context.Context, key, expectedValue string, ttl time.Duration) (bool, error) {
	if m.CompareAndExpireFunc != nil {
		return m.CompareAndExpireFunc(ctx, key, expectedValue, ttl)
	}
	m.panicIfUnset("CompareAndExpireFunc")
	return false, nil
}
