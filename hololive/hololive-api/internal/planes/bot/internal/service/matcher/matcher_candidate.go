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

package matcher

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
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
		channelID:       member.ChannelID,
		memberName:      name,
		koreanName:      member.NameKo,
		shortKoreanName: member.ShortKoreanName,
		org:             member.GetOrg(),
		source:          source,
	}
}

// member는 후보를 표시·동명이인 안내용 Member로 되돌린다. Name은 검색 키라서 동명이인 안내의 복사 예시가 다시 같은 후보로 해석된다.
func (c *matchCandidate) member() *domain.Member {
	return &domain.Member{
		Name:            c.memberName,
		NameKo:          c.koreanName,
		ShortKoreanName: c.shortKoreanName,
		ChannelID:       c.channelID,
		Org:             c.org,
	}
}

// finalizeCandidate는 멤버 데이터 snapshot 후보에서 채널을 만든다. 채널명 정본은 roster다.
// Holodex GetChannel로 보강하던 경로와, 그 실패 시 후보명·Valkey 알림 멤버명 캐시로 채우던 폴백 체인은
// DEC-20260926-hololive-source-fallbacks-retirement로 지웠다. 정확 일치 경로(memberToChannel)와 같은 roster 값을 쓴다.
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
	return util.NormalizeSuffix(value)
}
