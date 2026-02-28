package member

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// ServiceAdapter: MemberCache를 래핑하여 domain.MemberDataProvider 인터페이스를 구현하는 어댑터
// 이를 통해 도메인 로직에서 구체적인 캐시 구현에 의존하지 않고 멤버 정보를 조회할 수 있다.
type ServiceAdapter struct {
	cache  *Cache
	ctx    context.Context
	logger *slog.Logger
}

// NewMemberServiceAdapter: 새로운 MemberServiceAdapter 인스턴스를 생성합니다.
func NewMemberServiceAdapter(cache *Cache, logger *slog.Logger) *ServiceAdapter {
	return &ServiceAdapter{
		cache:  cache,
		ctx:    context.Background(),
		logger: logger,
	}
}

// FindMemberByChannelID: MembersData 인터페이스 구현
func (a *ServiceAdapter) FindMemberByChannelID(channelID string) *domain.Member {
	member, err := a.cache.GetByChannelID(a.ctx, channelID)
	if err != nil {
		a.logger.Warn("cache lookup failed in FindMemberByChannelID", "channelID", channelID, "error", err)
		return nil
	}
	return member
}

// FindMemberByName: MembersData 인터페이스 구현
func (a *ServiceAdapter) FindMemberByName(name string) *domain.Member {
	member, err := a.cache.GetByName(a.ctx, name)
	if err != nil {
		a.logger.Warn("cache lookup failed in FindMemberByName", "name", name, "error", err)
		return nil
	}
	return member
}

// FindMemberByAlias: MembersData 인터페이스 구현
func (a *ServiceAdapter) FindMemberByAlias(alias string) *domain.Member {
	member, err := a.cache.FindByAlias(a.ctx, alias)
	if err != nil {
		a.logger.Warn("cache lookup failed in FindMemberByAlias", "alias", alias, "error", err)
		return nil
	}
	return member
}

// GetChannelIDs: MemberDataProvider 인터페이스 구현
func (a *ServiceAdapter) GetChannelIDs() []string {
	channelIDs, err := a.cache.GetAllChannelIDs(a.ctx)
	if err != nil {
		a.logger.Warn("cache lookup failed in GetChannelIDs", "error", err)
		return []string{}
	}
	return channelIDs
}

// GetAllMembers: MemberDataProvider 인터페이스 구현
func (a *ServiceAdapter) GetAllMembers() []*domain.Member {
	members, err := a.cache.repo.GetAllMembers(a.ctx)
	if err != nil {
		a.logger.Warn("repository lookup failed in GetAllMembers", "error", err)
		return []*domain.Member{}
	}
	return members
}

// WithContext: 커스텀 context를 가진 새 adapter를 생성합니다.
func (a *ServiceAdapter) WithContext(ctx context.Context) domain.MemberDataProvider {
	if ctx == nil {
		ctx = context.Background()
	}
	return &ServiceAdapter{
		cache:  a.cache,
		ctx:    ctx,
		logger: a.logger,
	}
}

// FindMembersByName: 이름으로 매칭되는 모든 멤버를 반환합니다.
func (a *ServiceAdapter) FindMembersByName(name string) []*domain.Member {
	return []*domain.Member{}
}

// FindMembersByAlias: 별명으로 매칭되는 모든 멤버를 반환합니다.
func (a *ServiceAdapter) FindMembersByAlias(alias string) []*domain.Member {
	return []*domain.Member{}
}
