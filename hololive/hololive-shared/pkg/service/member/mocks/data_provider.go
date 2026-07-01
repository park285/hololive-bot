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

type DataProvider struct {
	FindMemberByChannelIDFunc func(channelID string) *domain.Member
	FindMemberByNameFunc      func(name string) *domain.Member
	FindMemberByAliasFunc     func(alias string) *domain.Member
	GetChannelIDsFunc         func() []string
	GetAllMembersFunc         func() []*domain.Member
	WithContextFunc           func(ctx context.Context) domain.MemberDataProvider
	FindMembersByNameFunc     func(name string) []*domain.Member
	FindMembersByAliasFunc    func(alias string) []*domain.Member
}

var _ domain.MemberDataProvider = (*DataProvider)(nil)

func (m *DataProvider) FindMemberByChannelID(channelID string) *domain.Member {
	if m.FindMemberByChannelIDFunc != nil {
		return m.FindMemberByChannelIDFunc(channelID)
	}
	return nil
}

func (m *DataProvider) FindMemberByName(name string) *domain.Member {
	if m.FindMemberByNameFunc != nil {
		return m.FindMemberByNameFunc(name)
	}
	return nil
}

func (m *DataProvider) FindMemberByAlias(alias string) *domain.Member {
	if m.FindMemberByAliasFunc != nil {
		return m.FindMemberByAliasFunc(alias)
	}
	return nil
}

func (m *DataProvider) GetChannelIDs() []string {
	if m.GetChannelIDsFunc != nil {
		return m.GetChannelIDsFunc()
	}
	return nil
}

func (m *DataProvider) GetAllMembers() []*domain.Member {
	if m.GetAllMembersFunc != nil {
		return m.GetAllMembersFunc()
	}
	return nil
}

func (m *DataProvider) WithContext(ctx context.Context) domain.MemberDataProvider {
	if m.WithContextFunc != nil {
		return m.WithContextFunc(ctx)
	}
	return m
}

func (m *DataProvider) FindMembersByName(name string) []*domain.Member {
	if m.FindMembersByNameFunc != nil {
		return m.FindMembersByNameFunc(name)
	}
	return nil
}

func (m *DataProvider) FindMembersByAlias(alias string) []*domain.Member {
	if m.FindMembersByAliasFunc != nil {
		return m.FindMembersByAliasFunc(alias)
	}
	return nil
}
