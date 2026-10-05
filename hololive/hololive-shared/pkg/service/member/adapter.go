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

package member

import (
	"context"
	"errors"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// ServiceAdapter는 Cache를 domain.MemberDataProvider로 노출한다. 도메인 로직은 구체 캐시 구현에 의존하지 않고, 조회마다
// 호출자 ctx를 넘기며 미존재(domain.ErrMemberNotFound)와 조회 실패를 오류로 구분해 받는다.
type ServiceAdapter struct {
	cache *Cache
}

func NewMemberServiceAdapter(cache *Cache) *ServiceAdapter {
	return &ServiceAdapter{cache: cache}
}

var errMemberAdapterCacheNil = errors.New("member cache is nil")

func (a *ServiceAdapter) memberCache() (*Cache, error) {
	if a == nil || a.cache == nil {
		return nil, errMemberAdapterCacheNil
	}

	return a.cache, nil
}

func (a *ServiceAdapter) FindMemberByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	cache, err := a.memberCache()
	if err != nil {
		return nil, err
	}

	return cache.GetByChannelID(ctx, channelID)
}

func (a *ServiceAdapter) FindMemberByName(ctx context.Context, name string) (*domain.Member, error) {
	cache, err := a.memberCache()
	if err != nil {
		return nil, err
	}

	return cache.GetByName(ctx, name)
}

func (a *ServiceAdapter) FindMemberByAlias(ctx context.Context, alias string) (*domain.Member, error) {
	cache, err := a.memberCache()
	if err != nil {
		return nil, err
	}

	return cache.FindByAlias(ctx, alias)
}

func (a *ServiceAdapter) GetChannelIDs(ctx context.Context) ([]string, error) {
	cache, err := a.memberCache()
	if err != nil {
		return nil, err
	}

	return cache.GetAllChannelIDs(ctx)
}

func (a *ServiceAdapter) LoadAllMembers(ctx context.Context) ([]*domain.Member, error) {
	cache, err := a.memberCache()
	if err != nil {
		return nil, err
	}

	return cache.AllMembers(ctx)
}

func (a *ServiceAdapter) FindMembersByName(ctx context.Context, name string) ([]*domain.Member, error) {
	cache, err := a.memberCache()
	if err != nil {
		return nil, err
	}

	return cache.MembersByName(ctx, name)
}

func (a *ServiceAdapter) FindMembersByAlias(ctx context.Context, alias string) ([]*domain.Member, error) {
	cache, err := a.memberCache()
	if err != nil {
		return nil, err
	}

	return cache.MembersByAlias(ctx, alias)
}
