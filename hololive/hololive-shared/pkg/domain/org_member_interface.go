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

package domain

import (
	"context"
	"errors"
)

// ErrMemberNotFound는 단건 조회에서 일치하는 멤버가 없다는 정상 결과다. 저장소·캐시 실패와 구분하도록 단건 Find*는
// 미존재를 (nil, nil)이 아니라 이 오류(감싼 형태 포함)로 돌려주며, 호출자는 errors.Is로 판별한다.
var ErrMemberNotFound = errors.New("member not found")

// MemberDataProvider는 정적 파일 데이터 또는 PostgreSQL 기반 동적 데이터 소스 추상화다. 모든 조회는 호출자 ctx를
// 직접 받아 취소를 따르며, 실패를 빈 결과로 바꾸지 않고 오류로 돌려준다.
type MemberDataProvider interface {
	MemberFinder
	MemberMultiFinder

	// GetChannelIDs는 채널 ID 목록이다. 조회 실패는 빈 목록이 아니라 오류다.
	GetChannelIDs(ctx context.Context) ([]string, error)
	// LoadAllMembers는 전체 멤버 순회 계약이다. repository·cache 실패는 빈 결과로 바꾸지 않고 오류로 돌려준다.
	// 오류를 흡수하던 GetAllMembers와 선택적 MemberDataLoader 병존은 DEC-20260926-hololive-source-fallbacks-retirement로
	// 이 메서드 하나로 합쳤다.
	LoadAllMembers(ctx context.Context) ([]*Member, error)
}

// MemberFinder는 단건 조회 계약이다. 일치하는 멤버가 없으면 ErrMemberNotFound를, 조회 자체가 실패하면 그 밖의 오류를
// 돌려준다. 성공이면 멤버는 nil이 아니다.
type MemberFinder interface {
	FindMemberByChannelID(ctx context.Context, channelID string) (*Member, error)
	FindMemberByName(ctx context.Context, name string) (*Member, error)
	FindMemberByAlias(ctx context.Context, alias string) (*Member, error)
}

// MemberMultiFinder는 동명이인·공유 별명을 모두 돌려주는 조회 계약이다. 일치하는 멤버가 없으면 nil 오류와 빈 slice를,
// 조회 실패는 오류를 돌려준다. 돌려준 slice는 호출자 소유다.
type MemberMultiFinder interface {
	FindMembersByName(ctx context.Context, name string) ([]*Member, error)
	FindMembersByAlias(ctx context.Context, alias string) ([]*Member, error)
}
