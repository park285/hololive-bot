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
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// 이를 통해 도메인 로직에서 구체적인 캐시 구현에 의존하지 않고 멤버 정보를 조회할 수 있다.
type ServiceAdapter struct {
	cache *Cache
	//nolint:containedctx // domain.MemberDataProvider의 조회 메서드가 ctx를 받지 않고 WithContext로만 컨텍스트를 넘기므로, 어댑터가 보관하는 수밖에 없다.
	ctx    context.Context
	logger *slog.Logger
}

func NewMemberServiceAdapter(ctx context.Context, cache *Cache, logger *slog.Logger) *ServiceAdapter {
	return &ServiceAdapter{
		cache:  cache,
		ctx:    ctx,
		logger: memberAdapterLogger(logger),
	}
}

func (a *ServiceAdapter) FindMemberByChannelID(channelID string) *domain.Member {
	member, err := a.cache.GetByChannelID(a.ctx, channelID)
	if err != nil {
		if !errors.Is(err, ErrMemberNotFound) {
			a.logger.Warn("cache lookup failed in FindMemberByChannelID", "channelID", channelID, "error", err)
		}

		return nil
	}

	return member
}

func (a *ServiceAdapter) FindMemberByName(name string) *domain.Member {
	member, err := a.cache.GetByName(a.ctx, name)
	if err != nil {
		if !errors.Is(err, ErrMemberNotFound) {
			a.logger.Warn("cache lookup failed in FindMemberByName", "name", name, "error", err)
		}

		return nil
	}

	return member
}

func (a *ServiceAdapter) FindMemberByAlias(alias string) *domain.Member {
	member, err := a.cache.FindByAlias(a.ctx, alias)
	if err != nil {
		if !errors.Is(err, ErrMemberNotFound) {
			a.logger.Warn("cache lookup failed in FindMemberByAlias", "alias", alias, "error", err)
		}

		return nil
	}

	return member
}

func (a *ServiceAdapter) GetChannelIDs() []string {
	channelIDs, err := a.cache.GetAllChannelIDs(a.ctx)
	if err != nil {
		a.logger.Warn("cache lookup failed in GetChannelIDs", "error", err)

		return []string{}
	}

	return channelIDs
}

func (a *ServiceAdapter) LoadAllMembers() ([]*domain.Member, error) {
	if a == nil {
		return nil, errors.New("member adapter is nil")
	}

	if a.cache == nil {
		return nil, errors.New("member cache is nil")
	}

	members, err := a.cache.AllMembers(a.ctx)
	if err != nil {
		return nil, err
	}

	return members, nil
}

func (a *ServiceAdapter) WithContext(ctx context.Context) domain.MemberDataProvider {
	return &ServiceAdapter{
		cache:  a.cache,
		ctx:    ctx,
		logger: memberAdapterLogger(a.logger),
	}
}

func (a *ServiceAdapter) FindMembersByName(name string) []*domain.Member {
	needle := strings.TrimSpace(name)
	if needle == "" {
		return []*domain.Member{}
	}

	members := a.searchableMembers()
	matched := make([]*domain.Member, 0, len(members))

	for _, member := range members {
		if member == nil {
			continue
		}

		if equalFoldAny(needle, member.Name, member.NameJa, member.NameKo) {
			matched = append(matched, member)
		}
	}

	return cloneMemberSlice(matched)
}

func (a *ServiceAdapter) FindMembersByAlias(alias string) []*domain.Member {
	needle := strings.TrimSpace(alias)
	if needle == "" {
		return []*domain.Member{}
	}

	members := a.searchableMembers()
	matched := make([]*domain.Member, 0, len(members))

	for _, member := range members {
		if memberHasAlias(member, needle) {
			matched = append(matched, member)
		}
	}

	return cloneMemberSlice(matched)
}

// MemberMultiFinder(FindMembersByName/Alias)의 조회 원천이다. Finder 계약은 오류를 돌려주지 않으므로 다른 Find* 메서드처럼
// cache 실패를 경고로 남기고 빈 결과를 쓴다. 전체 멤버가 필요한 호출자는 LoadAllMembers로 오류를 받는다.
func (a *ServiceAdapter) searchableMembers() []*domain.Member {
	if a == nil || a.cache == nil {
		return []*domain.Member{}
	}

	members, err := a.LoadAllMembers()
	if err != nil {
		a.logger.Warn("cache lookup failed in searchableMembers", "error", err)

		return nil
	}

	return members
}

func memberHasAlias(member *domain.Member, needle string) bool {
	if member == nil {
		return false
	}

	for _, candidate := range member.GetAllAliases() {
		if strings.EqualFold(strings.TrimSpace(candidate), needle) {
			return true
		}
	}

	return false
}

func equalFoldAny(target string, values ...string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}

	return false
}

func cloneMemberSlice(in []*domain.Member) []*domain.Member {
	if len(in) == 0 {
		return []*domain.Member{}
	}

	out := make([]*domain.Member, len(in))
	copy(out, in)

	return out
}

func memberAdapterLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}

	return logger
}
