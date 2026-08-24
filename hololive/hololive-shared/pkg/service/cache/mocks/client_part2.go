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
	"fmt"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

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
