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
	"fmt"
	"slices"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// GetByChannelID는 채널 대표(가장 작은 영속 ID) 멤버를 돌려준다. 없으면 domain.ErrMemberNotFound를 감싼 오류다.
func (c *Cache) GetByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	return c.lookupPoint(ctx, pointLookupChannel, channelID)
}

// GetByName은 english_name 정확 일치 멤버를 돌려준다. 없으면 domain.ErrMemberNotFound를 감싼 오류다.
func (c *Cache) GetByName(ctx context.Context, name string) (*domain.Member, error) {
	return c.lookupPoint(ctx, pointLookupName, name)
}

// FindByAlias는 게시된 snapshot에서 별칭을 먼저 찾는다. Snapshot이 없거나 snapshot에 없는 별칭은 예전 L2 miss와 같이
// PostgreSQL을 조회한다(epoch 무효화 없이 바뀐 행도 곧바로 보이도록 음성 결과는 캐시하지 않는다). 없으면
// domain.ErrMemberNotFound를 감싼 오류다.
func (c *Cache) FindByAlias(ctx context.Context, alias string) (*domain.Member, error) {
	return c.lookupPoint(ctx, pointLookupAlias, alias)
}

type pointLookupKind uint8

const (
	pointLookupChannel pointLookupKind = iota
	pointLookupName
	pointLookupAlias
)

func (k pointLookupKind) operation() string {
	switch k {
	case pointLookupChannel:
		return "channel"
	case pointLookupName:
		return "name"
	case pointLookupAlias:
		return "alias"
	}

	return "unknown"
}

// lookupPoint는 epoch이 불확실하면 PostgreSQL을 직접 읽고, 아니면 메모리 miss에서만 PostgreSQL을 읽어 조회 시점
// generation이 그대로일 때만 overlay에 게시한다. 미존재는 캐시하지 않는다.
func (c *Cache) lookupPoint(ctx context.Context, kind pointLookupKind, key string) (*domain.Member, error) {
	if c == nil {
		return nil, errors.New("member cache is nil")
	}

	if c.cacheBypassRequired(kind.operation()) {
		return c.findPointInRepository(ctx, kind, key)
	}

	member, generation := c.lookupPointInMemory(kind, key)
	if member != nil {
		return member, nil
	}

	dbMember, err := c.findPointInRepository(ctx, kind, key)
	if err != nil {
		return nil, err
	}

	c.cacheMember(dbMember, generation, kind == pointLookupChannel)

	return dbMember, nil
}

// lookupPointInMemory는 같은 잠금 안에서 게시된 snapshot, point overlay 순으로 찾고, miss면 PostgreSQL 결과를 게시할 때
// 쓸 generation을 함께 돌려준다. 별칭은 snapshot 규칙만 쓰고 overlay를 보지 않는다(예전 동작 유지).
func (c *Cache) lookupPointInMemory(kind pointLookupKind, key string) (*domain.Member, uint64) {
	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()

	generation := c.snapshotGeneration.Load()

	if snap := c.currentSnapshotLocked(generation); snap != nil {
		var member *domain.Member

		switch kind {
		case pointLookupChannel:
			member = snap.index.channelRepresentatives[key]
		case pointLookupName:
			member = snap.index.byName[key]
		case pointLookupAlias:
			member = snap.index.aliasOwner(key)
		}

		if member != nil {
			return member, generation
		}
	}

	overlay := c.currentOverlayLocked(generation)
	if overlay == nil {
		return nil, generation
	}

	switch kind {
	case pointLookupChannel:
		return overlay.byChannelID[key], generation
	case pointLookupName:
		return overlay.byName[key], generation
	case pointLookupAlias:
		// 별칭은 snapshot 규칙만 쓰고 overlay를 보지 않는다.
	}

	return nil, generation
}

func (c *Cache) findPointInRepository(ctx context.Context, kind pointLookupKind, key string) (*domain.Member, error) {
	if c.repository == nil {
		return nil, errors.New("member repository is nil")
	}

	var (
		member *domain.Member
		err    error
	)

	switch kind {
	case pointLookupChannel:
		member, err = c.repository.FindByChannelID(ctx, key)
	case pointLookupName:
		member, err = c.repository.FindByName(ctx, key)
	case pointLookupAlias:
		member, err = c.repository.FindByAlias(ctx, key)
	default:
		return nil, fmt.Errorf("unknown member point lookup kind %d", kind)
	}

	if err != nil {
		return nil, fmt.Errorf("find member by %s: %w", kind.operation(), err)
	}

	if member == nil {
		return nil, fmt.Errorf("find member by %s: %w", kind.operation(), domain.ErrMemberNotFound)
	}

	return member, nil
}

// MembersByName은 english/japanese/korean 이름 중 하나가 앞뒤 공백을 무시한 대소문자 무관 비교(strings.EqualFold)로
// 일치하는 멤버를 모두 snapshot 순서대로 돌려준다. 공백뿐인 이름은 적재 없이 빈 결과다.
func (c *Cache) MembersByName(ctx context.Context, name string) ([]*domain.Member, error) {
	return c.searchMembers(ctx, "members_by_name", name, (*memberSnapshotIndex).membersByName)
}

// MembersByAlias는 명시 별칭(ko·ja) 중 하나가 앞뒤 공백을 무시한 대소문자 무관 비교(strings.EqualFold)로 일치하는 멤버를
// 모두 snapshot 순서대로 돌려준다. 공백뿐인 별칭은 적재 없이 빈 결과다.
func (c *Cache) MembersByAlias(ctx context.Context, alias string) ([]*domain.Member, error) {
	return c.searchMembers(ctx, "members_by_alias", alias, (*memberSnapshotIndex).membersByAlias)
}

func (c *Cache) searchMembers(
	ctx context.Context,
	operation, query string,
	lookup func(*memberSnapshotIndex, string) []*domain.Member,
) ([]*domain.Member, error) {
	key, ok := searchKey(query)
	if !ok {
		return []*domain.Member{}, nil
	}

	snap, err := c.membersSnapshot(ctx, operation)
	if err != nil {
		return nil, err
	}

	return cloneMemberSlice(lookup(snap.index, key)), nil
}

// GetAllChannelIDs는 snapshot의 채널 ID 목록을, snapshot이 없으면 PostgreSQL 목록을 돌려준다. 돌려준 slice는 호출자 소유다.
func (c *Cache) GetAllChannelIDs(ctx context.Context) ([]string, error) {
	if c == nil {
		return nil, errors.New("member cache is nil")
	}

	if c.cacheBypassRequired("channel_ids") {
		return c.channelIDsFromRepository(ctx)
	}

	channelIDs, generation, ok := c.channelIDsInMemory()
	if ok {
		return slices.Clone(channelIDs), nil
	}

	channelIDs, err := c.channelIDsFromRepository(ctx)
	if err != nil {
		return nil, err
	}

	c.cacheChannelIDs(channelIDs, generation)

	return slices.Clone(channelIDs), nil
}

func (c *Cache) channelIDsInMemory() ([]string, uint64, bool) {
	c.snapshotMu.RLock()
	defer c.snapshotMu.RUnlock()

	generation := c.snapshotGeneration.Load()

	if snap := c.currentSnapshotLocked(generation); snap != nil {
		return snap.index.channelIDs, generation, true
	}

	if overlay := c.currentOverlayLocked(generation); overlay != nil && overlay.hasChannelIDs {
		return overlay.channelIDs, generation, true
	}

	return nil, generation, false
}

func (c *Cache) channelIDsFromRepository(ctx context.Context) ([]string, error) {
	if c.repository == nil {
		return nil, errors.New("member repository is nil")
	}

	channelIDs, err := c.repository.GetAllChannelIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("get all channel IDs: %w", err)
	}

	return channelIDs, nil
}

// currentSnapshotLocked는 현재 generation에 게시된 성공 snapshot을 돌려준다. 호출자가 snapshotMu를 쥔다.
func (c *Cache) currentSnapshotLocked(generation uint64) *allMembersState {
	snap := c.allMembersSnapshot.Load()
	if !snapshotSuccessful(snap) || snap.generation != generation {
		return nil
	}

	return snap
}
