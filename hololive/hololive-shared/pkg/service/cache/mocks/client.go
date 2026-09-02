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
	"fmt"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type Client struct {
	Lenient bool

	GetFunc       func(ctx context.Context, key string, dest any) error
	GetStringFunc func(ctx context.Context, key string) (string, bool, error)
	SetFunc       func(ctx context.Context, key string, value any, ttl time.Duration) error
	MSetFunc      func(ctx context.Context, pairs map[string]any, ttl time.Duration) error
	DelFunc       func(ctx context.Context, key string) error
	DelManyFunc   func(ctx context.Context, keys []string) (int64, error)
	ScanKeysFunc  func(ctx context.Context, pattern string, batchSize int64) ([]string, error)

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

	SetNXMultiFunc func(ctx context.Context, entries []cache.SetNXEntry) ([]cache.SetNXResult, error)

	CompareAndDeleteFunc func(ctx context.Context, key, expectedValue string) (bool, error)
	CompareAndExpireFunc func(ctx context.Context, key, expectedValue string, ttl time.Duration) (bool, error)

	GetStreamsFunc func(ctx context.Context, key string) ([]*domain.Stream, bool)
	SetStreamsFunc func(ctx context.Context, key string, streams []*domain.Stream, ttl time.Duration)

	InitializeMemberDatabaseFunc func(ctx context.Context, memberData map[string]string) error
	GetAllMembersFunc            func(ctx context.Context) (map[string]string, error)
}

var (
	_ cache.Client            = (*Client)(nil)
	_ cache.KeyValueCache     = (*Client)(nil)
	_ cache.SetCache          = (*Client)(nil)
	_ cache.HashCache         = (*Client)(nil)
	_ cache.ScriptCache       = (*Client)(nil)
	_ cache.StreamCache       = (*Client)(nil)
	_ cache.MemberCache       = (*Client)(nil)
	_ cache.ConnectionManager = (*Client)(nil)
	_ cache.LowLevelCache     = (*Client)(nil)
)

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

func (m *Client) Get(ctx context.Context, key string, dest any) error {
	if m.GetFunc != nil {
		if err := m.GetFunc(ctx, key, dest); err != nil {
			return fmt.Errorf("get func: %w", err)
		}

		return nil
	}

	m.panicIfUnset("GetFunc")

	return nil
}

func (m *Client) GetString(ctx context.Context, key string) (value0 string, ok1 bool, err error) {
	if m.GetStringFunc != nil {
		out1, out2, err := m.GetStringFunc(ctx, key)
		if err != nil {
			return out1, out2, fmt.Errorf("get string func: %w", err)
		}

		return out1, out2, nil
	}

	m.panicIfUnset("GetStringFunc")

	return "", false, nil
}

func (m *Client) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if m.SetFunc != nil {
		if err := m.SetFunc(ctx, key, value, ttl); err != nil {
			return fmt.Errorf("set func: %w", err)
		}

		return nil
	}

	m.panicIfUnset("SetFunc")

	return nil
}

func (m *Client) MSet(ctx context.Context, pairs map[string]any, ttl time.Duration) error {
	if m.MSetFunc != nil {
		if err := m.MSetFunc(ctx, pairs, ttl); err != nil {
			return fmt.Errorf("m set func: %w", err)
		}

		return nil
	}

	m.panicIfUnset("MSetFunc")

	return nil
}

func (m *Client) Del(ctx context.Context, key string) error {
	if m.DelFunc != nil {
		if err := m.DelFunc(ctx, key); err != nil {
			return err
		}

		return nil
	}

	m.panicIfUnset("DelFunc")

	return nil
}

func (m *Client) DelMany(ctx context.Context, keys []string) (int64, error) {
	if m.DelManyFunc != nil {
		out, err := m.DelManyFunc(ctx, keys)
		if err != nil {
			return out, fmt.Errorf("del many func: %w", err)
		}

		return out, nil
	}

	m.panicIfUnset("DelManyFunc")

	return 0, nil
}

func (m *Client) ScanKeys(ctx context.Context, pattern string, batchSize int64) ([]string, error) {
	if m.ScanKeysFunc != nil {
		out, err := m.ScanKeysFunc(ctx, pattern, batchSize)
		if err != nil {
			return out, fmt.Errorf("scan keys func: %w", err)
		}

		return out, nil
	}

	m.panicIfUnset("ScanKeysFunc")

	return nil, nil
}

func (m *Client) SAdd(ctx context.Context, key string, members []string) (int64, error) {
	if m.SAddFunc != nil {
		out, err := m.SAddFunc(ctx, key, members)
		if err != nil {
			return out, err
		}

		return out, nil
	}

	m.panicIfUnset("SAddFunc")

	return 0, nil
}

func (m *Client) SRem(ctx context.Context, key string, members []string) (int64, error) {
	if m.SRemFunc != nil {
		out, err := m.SRemFunc(ctx, key, members)
		if err != nil {
			return out, err
		}

		return out, nil
	}

	m.panicIfUnset("SRemFunc")

	return 0, nil
}

func (m *Client) SMembers(ctx context.Context, key string) ([]string, error) {
	if m.SMembersFunc != nil {
		out, err := m.SMembersFunc(ctx, key)
		if err != nil {
			return out, fmt.Errorf("s members func: %w", err)
		}

		return out, nil
	}

	m.panicIfUnset("SMembersFunc")

	return nil, nil
}

func (m *Client) SIsMember(ctx context.Context, key, member string) (bool, error) {
	if m.SIsMemberFunc != nil {
		out, err := m.SIsMemberFunc(ctx, key, member)
		if err != nil {
			return out, fmt.Errorf("s is member func: %w", err)
		}

		return out, nil
	}

	m.panicIfUnset("SIsMemberFunc")

	return false, nil
}

func (m *Client) HSet(ctx context.Context, key, field, value string) error {
	if m.HSetFunc != nil {
		if err := m.HSetFunc(ctx, key, field, value); err != nil {
			return fmt.Errorf("h set func: %w", err)
		}

		return nil
	}

	m.panicIfUnset("HSetFunc")

	return nil
}

func (m *Client) HMSet(ctx context.Context, key string, fields map[string]any) error {
	if m.HMSetFunc != nil {
		if err := m.HMSetFunc(ctx, key, fields); err != nil {
			return fmt.Errorf("HM set func: %w", err)
		}

		return nil
	}

	m.panicIfUnset("HMSetFunc")

	return nil
}

func (m *Client) HGet(ctx context.Context, key, field string) (string, error) {
	if m.HGetFunc != nil {
		out, err := m.HGetFunc(ctx, key, field)
		if err != nil {
			return out, fmt.Errorf("h get func: %w", err)
		}

		return out, nil
	}

	m.panicIfUnset("HGetFunc")

	return "", nil
}

func (m *Client) BatchHGet(ctx context.Context, key string, fields []string) (map[string]string, error) {
	if m.BatchHGetFunc != nil {
		out, err := m.BatchHGetFunc(ctx, key, fields)
		if err != nil {
			return nil, fmt.Errorf("batch h get func: %w", err)
		}

		return out, nil
	}

	m.panicIfUnset("BatchHGetFunc")

	//nolint:nilnil // lenient mock의 미설정 기본값은 제로값이라는 계약이다. sentinel 오류를 내보내면 이 mock을 쓰는 다른 패키지 테스트가 실패 경로로 갈라진다.
	return nil, nil
}

func (m *Client) HDel(ctx context.Context, key string, fields ...string) error {
	if m.HDelFunc != nil {
		if err := m.HDelFunc(ctx, key, fields...); err != nil {
			return fmt.Errorf("h del func: %w", err)
		}

		return nil
	}

	m.panicIfUnset("HDelFunc")

	return nil
}

func (m *Client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	if m.HGetAllFunc != nil {
		out, err := m.HGetAllFunc(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("h get all func: %w", err)
		}

		return out, nil
	}

	m.panicIfUnset("HGetAllFunc")

	//nolint:nilnil // lenient mock의 미설정 기본값은 제로값이라는 계약이다. sentinel 오류를 내보내면 이 mock을 쓰는 다른 패키지 테스트가 실패 경로로 갈라진다.
	return nil, nil
}

func (m *Client) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if m.ExpireFunc != nil {
		if err := m.ExpireFunc(ctx, key, ttl); err != nil {
			return fmt.Errorf("expire func: %w", err)
		}

		return nil
	}

	m.panicIfUnset("ExpireFunc")

	return nil
}

func (m *Client) Exists(ctx context.Context, key string) (bool, error) {
	if m.ExistsFunc != nil {
		out, err := m.ExistsFunc(ctx, key)
		if err != nil {
			return out, fmt.Errorf("exists func: %w", err)
		}

		return out, nil
	}

	m.panicIfUnset("ExistsFunc")

	return false, nil
}

func (m *Client) Close() error {
	if m.CloseFunc != nil {
		if err := m.CloseFunc(); err != nil {
			return fmt.Errorf("close func: %w", err)
		}

		return nil
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
		if err := m.WaitUntilReadyFunc(ctx, timeout); err != nil {
			return fmt.Errorf("wait until ready func: %w", err)
		}

		return nil
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
		out, err := m.SetNXFunc(ctx, key, value, ttl)
		if err != nil {
			return out, fmt.Errorf("set NX func: %w", err)
		}

		return out, nil
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

func (m *Client) SetNXMulti(ctx context.Context, entries []cache.SetNXEntry) ([]cache.SetNXResult, error) {
	if m.SetNXMultiFunc != nil {
		out, err := m.SetNXMultiFunc(ctx, entries)
		if err != nil {
			return out, fmt.Errorf("set NX multi func: %w", err)
		}

		return out, nil
	}

	m.panicIfUnset("SetNXMultiFunc")

	return nil, nil
}

func (m *Client) CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error) {
	if m.CompareAndDeleteFunc != nil {
		out, err := m.CompareAndDeleteFunc(ctx, key, expectedValue)
		if err != nil {
			return out, fmt.Errorf("compare and delete func: %w", err)
		}

		return out, nil
	}

	m.panicIfUnset("CompareAndDeleteFunc")

	return false, nil
}

func (m *Client) CompareAndExpire(ctx context.Context, key, expectedValue string, ttl time.Duration) (bool, error) {
	if m.CompareAndExpireFunc != nil {
		out, err := m.CompareAndExpireFunc(ctx, key, expectedValue, ttl)
		if err != nil {
			return out, fmt.Errorf("compare and expire func: %w", err)
		}

		return out, nil
	}

	m.panicIfUnset("CompareAndExpireFunc")

	return false, nil
}
