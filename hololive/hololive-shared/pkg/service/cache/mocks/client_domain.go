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

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (m *Client) GetStreams(ctx context.Context, key string) ([]*domain.Stream, bool) {
	if m.GetStreamsFunc != nil {
		return m.GetStreamsFunc(ctx, key)
	}

	m.panicIfUnset("GetStreamsFunc")

	return nil, false
}

func (m *Client) SetStreams(ctx context.Context, key string, streams []*domain.Stream, ttl time.Duration) {
	if m.SetStreamsFunc != nil {
		m.SetStreamsFunc(ctx, key, streams, ttl)

		return
	}

	m.panicIfUnset("SetStreamsFunc")
}

func (m *Client) InitializeMemberDatabase(ctx context.Context, memberData map[string]string) error {
	if m.InitializeMemberDatabaseFunc != nil {
		if err := m.InitializeMemberDatabaseFunc(ctx, memberData); err != nil {
			return fmt.Errorf("initialize member database func: %w", err)
		}

		return nil
	}

	m.panicIfUnset("InitializeMemberDatabaseFunc")

	return nil
}

func (m *Client) GetAllMembers(ctx context.Context) (map[string]string, error) {
	if m.GetAllMembersFunc != nil {
		out, err := m.GetAllMembersFunc(ctx)
		if err != nil {
			return nil, fmt.Errorf("get all members func: %w", err)
		}

		return out, nil
	}

	m.panicIfUnset("GetAllMembersFunc")

	//nolint:nilnil // lenient mock의 미설정 기본값은 제로값이라는 계약이다. sentinel 오류를 내보내면 이 mock을 쓰는 다른 패키지 테스트가 실패 경로로 갈라진다.
	return nil, nil
}
