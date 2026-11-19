package matcher

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (mm *Matcher) candidateFromMember(member *domain.Member, source string) *matchCandidate {
	if member == nil || member.ChannelID == "" {
		return nil
	}

	name := member.Name
	if name == "" {
		name = member.NameJa
	}

	if name == "" {
		name = member.ChannelID
	}

	return &matchCandidate{
		memberID: member.ID, photo: member.Photo, graduated: member.IsGraduated,
		channelID:       member.ChannelID,
		memberName:      name,
		koreanName:      member.NameKo,
		shortKoreanName: member.ShortKoreanName,
		org:             member.GetOrg(),
		source:          source,
	}
}

func (c *matchCandidate) member() *domain.Member {
	if c == nil {
		return nil
	}

	return &domain.Member{
		ID: c.memberID, Photo: c.photo, IsGraduated: c.graduated,
		Name:            c.memberName,
		NameKo:          c.koreanName,
		ShortKoreanName: c.shortKoreanName,
		ChannelID:       c.channelID,
		Org:             c.org,
	}
}

func (mm *Matcher) finalizeCandidate(candidate *matchCandidate) *domain.Channel {
	if candidate == nil {
		return nil
	}

	if candidate.channelID == "" {
		mm.logger.Warn("Match candidate missing channel ID",
			slog.String("member", candidate.memberName),
			slog.String("source", candidate.source),
		)

		return nil
	}

	mm.logger.Debug("Match candidate resolved",
		slog.String("channel_id", candidate.channelID),
		slog.String("member", candidate.memberName),
		slog.String("source", candidate.source),
	)

	channel := &domain.Channel{
		ID:   candidate.channelID,
		Name: candidate.member().DisplayName(),
	}
	if candidate.org != "" {
		channel.Org = toStringPtr(candidate.org)
	}

	return channel
}

func toStringPtr(value string) *string {
	if value == "" {
		return nil
	}

	copied := value

	return &copied
}

func normalizeMatcherTerm(value string) string {
	return NormalizeSuffix(value)
}
