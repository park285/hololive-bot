package member

import "github.com/kapu/hololive-shared/pkg/domain"

// ChannelRepresentatives는 channel ID별 대표 멤버를 돌려준다. 빈 channel ID와 nil 멤버는 제외한다.
// 채널 대표는 최초 등록된 영속 ID의 행이다. 이름·별칭 조회 순서로 대표가 바뀌면 안 된다.
// SQL 채널 조회의 ORDER BY id와 같은 규칙을 메모리·분산 캐시와 YouTube 채널 이름에 적용한다.
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

// channelMemberForPointLocked는 point 결과로 채울 채널 키의 주인을 고른다. 호출자가 snapshotMu를 쥔다.
func (c *Cache) channelMemberForPointLocked(member *domain.Member, generation uint64, channelLookup bool) *domain.Member {
	if snap := c.currentSnapshotLocked(generation); snap != nil {
		return snap.index.channelRepresentatives[member.ChannelID]
	}

	// 스냅샷이 없을 때는 SQL의 채널 대표 조회 결과만 채널 인덱스를 채울 수 있다.
	if channelLookup {
		return member
	}

	return nil
}
