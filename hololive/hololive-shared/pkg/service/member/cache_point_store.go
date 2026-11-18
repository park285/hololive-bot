package member

import (
	"github.com/kapu/hololive-shared/pkg/domain"
)

type memberPointOverlay struct {
	byChannelID   map[string]*domain.Member
	byName        map[string]*domain.Member
	channelIDs    []string
	generation    uint64
	hasChannelIDs bool
}

func (c *Cache) currentOverlayLocked(generation uint64) *memberPointOverlay {
	if c.pointOverlay == nil || c.pointOverlay.generation != generation {
		return nil
	}

	return c.pointOverlay
}

func (c *Cache) overlayForWriteLocked(generation uint64) *memberPointOverlay {
	if c.snapshotGeneration.Load() != generation {
		return nil
	}

	if overlay := c.currentOverlayLocked(generation); overlay != nil {
		return overlay
	}

	c.pointOverlay = &memberPointOverlay{
		byChannelID: make(map[string]*domain.Member),
		byName:      make(map[string]*domain.Member),
		generation:  generation,
	}

	return c.pointOverlay
}

func (c *Cache) cacheMember(member *domain.Member, generation uint64, channelLookup bool) {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()

	overlay := c.overlayForWriteLocked(generation)
	if overlay == nil {
		return
	}

	overlay.byName[member.Name] = member

	channelMember := c.channelMemberForPointLocked(member, generation, channelLookup)
	if channelMember != nil && channelMember.ChannelID != "" {
		overlay.byChannelID[channelMember.ChannelID] = channelMember
	}
}

func (c *Cache) cacheChannelIDs(channelIDs []string, generation uint64) {
	c.snapshotMu.Lock()
	defer c.snapshotMu.Unlock()

	overlay := c.overlayForWriteLocked(generation)
	if overlay == nil {
		return
	}

	overlay.channelIDs = channelIDs
	overlay.hasChannelIDs = true
}
