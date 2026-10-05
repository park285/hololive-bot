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

	"github.com/kapu/hololive-shared/pkg/domain"
)

// DataProvider는 domain.MemberDataProvider 테스트 대역이다. 함수를 지정하지 않은 단건 조회는 미존재
// (domain.ErrMemberNotFound), 목록 조회는 빈 결과다.
type DataProvider struct {
	FindMemberByChannelIDFunc func(ctx context.Context, channelID string) (*domain.Member, error)
	FindMemberByNameFunc      func(ctx context.Context, name string) (*domain.Member, error)
	FindMemberByAliasFunc     func(ctx context.Context, alias string) (*domain.Member, error)
	GetChannelIDsFunc         func(ctx context.Context) ([]string, error)
	LoadAllMembersFunc        func(ctx context.Context) ([]*domain.Member, error)
	FindMembersByNameFunc     func(ctx context.Context, name string) ([]*domain.Member, error)
	FindMembersByAliasFunc    func(ctx context.Context, alias string) ([]*domain.Member, error)
}

var _ domain.MemberDataProvider = (*DataProvider)(nil)

func (m *DataProvider) FindMemberByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	if m.FindMemberByChannelIDFunc != nil {
		return m.FindMemberByChannelIDFunc(ctx, channelID)
	}

	return nil, domain.ErrMemberNotFound
}

func (m *DataProvider) FindMemberByName(ctx context.Context, name string) (*domain.Member, error) {
	if m.FindMemberByNameFunc != nil {
		return m.FindMemberByNameFunc(ctx, name)
	}

	return nil, domain.ErrMemberNotFound
}

func (m *DataProvider) FindMemberByAlias(ctx context.Context, alias string) (*domain.Member, error) {
	if m.FindMemberByAliasFunc != nil {
		return m.FindMemberByAliasFunc(ctx, alias)
	}

	return nil, domain.ErrMemberNotFound
}

func (m *DataProvider) GetChannelIDs(ctx context.Context) ([]string, error) {
	if m.GetChannelIDsFunc != nil {
		return m.GetChannelIDsFunc(ctx)
	}

	return []string{}, nil
}

func (m *DataProvider) LoadAllMembers(ctx context.Context) ([]*domain.Member, error) {
	if m.LoadAllMembersFunc != nil {
		return m.LoadAllMembersFunc(ctx)
	}

	return []*domain.Member{}, nil
}

func (m *DataProvider) FindMembersByName(ctx context.Context, name string) ([]*domain.Member, error) {
	if m.FindMembersByNameFunc != nil {
		return m.FindMembersByNameFunc(ctx, name)
	}

	return []*domain.Member{}, nil
}

func (m *DataProvider) FindMembersByAlias(ctx context.Context, alias string) ([]*domain.Member, error) {
	if m.FindMembersByAliasFunc != nil {
		return m.FindMembersByAliasFunc(ctx, alias)
	}

	return []*domain.Member{}, nil
}
