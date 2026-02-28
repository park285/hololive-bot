package domain

import "context"

// MemberDataProvider: 멤버 정보를 조회하는 다양한 메서드를 정의한 인터페이스
// 정적 파일 데이터 또는 Redis/DB 기반 동적 데이터 소스 추상화
type MemberDataProvider interface {
	FindMemberByChannelID(channelID string) *Member
	FindMemberByName(name string) *Member
	FindMemberByAlias(alias string) *Member
	GetChannelIDs() []string
	GetAllMembers() []*Member // 순회용 (레거시 호환성)
	WithContext(ctx context.Context) MemberDataProvider
	// Multi-result methods (동명이인/공유 별명 처리용)
	FindMembersByName(name string) []*Member
	FindMembersByAlias(alias string) []*Member
}
