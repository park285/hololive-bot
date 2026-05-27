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

package model

import (
	"context"
	"fmt"
)

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

// MemberDataLoader는 error-aware 전체 멤버 로드를 지원하는 선택적 확장 계약이다.
// critical path는 이 seam을 통해 repository/cache 실패를 빈 결과로 오해하지 않고 처리할 수 있다.
type MemberDataLoader interface {
	LoadAllMembers() ([]*Member, error)
}

// LoadAllMembers는 error-aware loader가 있으면 그 경로를 사용하고,
// 없으면 레거시 GetAllMembers() 동작으로 폴백한다.
func LoadAllMembers(provider MemberDataProvider) ([]*Member, error) {
	if provider == nil {
		return nil, nil
	}

	if loader, ok := provider.(MemberDataLoader); ok {
		members, err := loader.LoadAllMembers()
		if err != nil {
			return nil, fmt.Errorf("load all members: %w", err)
		}
		return members, nil
	}

	return provider.GetAllMembers(), nil
}
