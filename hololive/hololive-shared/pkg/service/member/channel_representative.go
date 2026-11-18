package member

import "github.com/kapu/hololive-shared/pkg/domain"

func ChannelRepresentatives(members []*domain.Member) map[string]*domain.Member {
	result := make(map[string]*domain.Member)

	for _, candidate := range members {
		if candidate == nil || candidate.ChannelID == "" {
			continue
		}

		current := result[candidate.ChannelID]
		if preferChannelMember(current, candidate) {
			result[candidate.ChannelID] = candidate
		}
	}

	return result
}

func preferChannelMember(current, candidate *domain.Member) bool {
	return candidate != nil && (current == nil || candidate.ID < current.ID)
}

func (c *Cache) channelMemberForPointLocked(member *domain.Member, generation uint64, channelLookup bool) *domain.Member {
	if snap := c.currentSnapshotLocked(generation); snap != nil {
		return snap.index.channelRepresentatives[member.ChannelID]
	}

	if channelLookup {
		return member
	}

	return nil
}
