package mocks

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// DataProvider is a manual mock for domain.MemberDataProvider (member.DataProvider alias).
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
