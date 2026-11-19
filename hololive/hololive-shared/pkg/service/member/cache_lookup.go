package member

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (c *Cache) FindMemberByChannelID(ctx context.Context, channelID string) (*domain.Member, error) {
	return c.lookupPoint(ctx, pointLookupChannel, channelID)
}

func (c *Cache) FindMemberByName(ctx context.Context, name string) (*domain.Member, error) {
	return c.lookupPoint(ctx, pointLookupName, name)
}

func (c *Cache) FindMemberByAlias(ctx context.Context, alias string) (*domain.Member, error) {
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

func (c *Cache) GetChannelIDs(ctx context.Context) ([]string, error) {
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

func (c *Cache) currentSnapshotLocked(generation uint64) *allMembersState {
	snap := c.allMembersSnapshot.Load()
	if !snapshotSuccessful(snap) || snap.generation != generation {
		return nil
	}

	return snap
}
