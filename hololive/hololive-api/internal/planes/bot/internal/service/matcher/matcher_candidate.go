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
	"context"
	"log/slog"
	"strings"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

// tryExactValkeyMatch: 동적 Valkey 데이터에서 정확한 매칭을 시도함 (Holodex 호출 없이).
func (mm *Matcher) tryExactValkeyMatch(provider domain.MemberDataProvider, query string, dynamicMembers map[string]string) *matchCandidate {
	var candidates []*matchCandidate

	for name, channelID := range dynamicMembers {
		if strings.EqualFold(name, query) {
			if candidate := mm.candidateFromDynamic(provider, name, channelID, "valkey-exact"); candidate != nil {
				candidates = append(candidates, candidate)
			}
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	if len(candidates) == 1 {
		return candidates[0]
	}

	if candidate := preferHololiveCandidate(provider, candidates); candidate != nil {
		return candidate
	}

	return candidates[0]
}

func preferHololiveCandidate(provider domain.MemberDataProvider, candidates []*matchCandidate) *matchCandidate {
	if provider == nil {
		return nil
	}

	for _, candidate := range candidates {
		if member := provider.FindMemberByChannelID(candidate.channelID); member != nil && member.Org == orgHololive {
			return candidate
		}
	}

	return nil
}

// tryPartialValkeyMatch: 동적 Valkey 데이터에서 부분 매칭을 시도함.
func (mm *Matcher) tryPartialValkeyMatch(provider domain.MemberDataProvider, queryNorm string, dynamicMembers map[string]string) *matchCandidate {
	for name, channelID := range dynamicMembers {
		nameNorm := stringutil.Normalize(name)
		if strings.Contains(nameNorm, queryNorm) || strings.Contains(queryNorm, nameNorm) {
			return mm.candidateFromDynamic(provider, name, channelID, "valkey-partial")
		}
	}

	return nil
}

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
		channelID:  member.ChannelID,
		memberName: name,
		org:        member.GetOrg(),
		source:     source,
	}
}

func (mm *Matcher) candidateFromDynamic(provider domain.MemberDataProvider, name, channelID, source string) *matchCandidate {
	if channelID == "" {
		return nil
	}

	if provider != nil {
		if member := provider.FindMemberByChannelID(channelID); member != nil {
			if candidate := mm.candidateFromMember(member, source); candidate != nil {
				return candidate
			}
		}
	}

	displayName := name
	if displayName == "" {
		displayName = channelID
	}

	return &matchCandidate{
		channelID:  channelID,
		memberName: displayName,
		org:        "",
		source:     source,
	}
}

// finalizeCandidate는 roster 후보(멤버 데이터, Valkey 동적 멤버)에서 채널을 만든다. 채널명 정본은 roster다.
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
		Name: candidate.memberName,
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

// loadDynamicMembers: Valkey 캐시에서 멤버 데이터를 로드함.
func (mm *Matcher) loadDynamicMembers(ctx context.Context) map[string]string {
	members, err := mm.cache.GetAllMembers(ctx)
	if err != nil {
		mm.logger.Warn("Failed to load dynamic members", slog.Any("error", err))

		return map[string]string{}
	}

	return members
}

func normalizeMatcherTerm(value string) string {
	return util.NormalizeSuffix(value)
}
