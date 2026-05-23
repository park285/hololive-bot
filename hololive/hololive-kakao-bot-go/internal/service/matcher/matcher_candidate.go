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

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
	"github.com/park285/hololive-bot/shared-go/pkg/stringutil"
)

// tryExactValkeyMatch: 동적 Valkey 데이터에서 정확한 매칭을 시도함 (Holodex 호출 없이).
func (mm *Matcher) tryExactValkeyMatch(provider domain.MemberDataProvider, query string, dynamicMembers map[string]string) *matchCandidate {
	var candidates []*matchCandidate

	for name, channelID := range dynamicMembers {
		if strings.EqualFold(name, query) {
			candidates = append(candidates, mm.candidateFromDynamic(provider, name, channelID, "valkey-exact"))
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
		if member := provider.FindMemberByChannelID(candidate.channelID); member != nil && member.Org == "Hololive" {
			return candidate
		}
	}
	return nil
}

// tryPartialStaticMatch: 정적 멤버 데이터에서 부분 매칭을 시도함.
func (mm *Matcher) tryPartialStaticMatch(provider domain.MemberDataProvider, queryNorm string) *matchCandidate {
	if provider != nil {
		for _, member := range provider.GetAllMembers() {
			nameNorm := stringutil.Normalize(member.Name)
			if strings.Contains(nameNorm, queryNorm) || strings.Contains(queryNorm, nameNorm) {
				return mm.candidateFromMember(member, "static-partial")
			}
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

// tryPartialAliasMatch: 모든 별칭에서 부분 매칭을 시도함.
func (mm *Matcher) tryPartialAliasMatch(provider domain.MemberDataProvider, queryNorm string) *matchCandidate {
	if provider != nil {
		for _, member := range provider.GetAllMembers() {
			if memberHasPartialAliasMatch(member, queryNorm) {
				return mm.candidateFromMember(member, "alias-partial")
			}
		}
	}

	return nil
}

func memberHasPartialAliasMatch(member *domain.Member, queryNorm string) bool {
	for _, alias := range member.GetAllAliases() {
		aliasNorm := stringutil.Normalize(alias)
		if strings.Contains(aliasNorm, queryNorm) || strings.Contains(queryNorm, aliasNorm) {
			return true
		}
	}
	return false
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

func (mm *Matcher) hydrateChannel(ctx context.Context, candidate *matchCandidate) (*domain.Channel, error) {
	if candidate == nil {
		return nil, nil
	}

	fallback := fallbackChannelFromCandidate(candidate)

	if mm.holodex == nil {
		return fallback, nil
	}

	channel, err := mm.holodex.GetChannel(ctx, candidate.channelID)
	if err != nil {
		mm.logger.Warn("Failed to fetch channel from Holodex",
			slog.String("channel_id", candidate.channelID),
			slog.String("source", candidate.source),
			slog.Any("error", err),
		)

		mm.applyCachedChannelNameFallback(ctx, fallback, candidate)

		return fallback, nil
	}

	if channel == nil {
		mm.logger.Warn("Holodex returned empty channel",
			slog.String("channel_id", candidate.channelID),
			slog.String("source", candidate.source),
		)

		return fallback, nil
	}

	applyCandidateNameFallback(channel, candidate)

	return channel, nil
}

func fallbackChannelFromCandidate(candidate *matchCandidate) *domain.Channel {
	fallback := &domain.Channel{
		ID:   candidate.channelID,
		Name: candidate.memberName,
	}
	if candidate.memberName != "" {
		fallback.EnglishName = toStringPtr(candidate.memberName)
	}
	return fallback
}

func (mm *Matcher) applyCachedChannelNameFallback(ctx context.Context, fallback *domain.Channel, candidate *matchCandidate) {
	if mm.cache == nil {
		return
	}
	cachedName, cacheErr := mm.cache.HGet(ctx, constants.RedisKeys.AlarmMemberNames, candidate.channelID)
	if cacheErr != nil || cachedName == "" {
		return
	}
	fallback.Name = cachedName
	mm.logger.Debug("Using cached channel name as fallback",
		slog.String("channel_id", candidate.channelID),
		slog.String("cached_name", cachedName),
	)
}

func applyCandidateNameFallback(channel *domain.Channel, candidate *matchCandidate) {
	if candidate.memberName == "" {
		return
	}
	if channel.Name == "" {
		channel.Name = candidate.memberName
	}
	if channel.EnglishName == nil {
		channel.EnglishName = toStringPtr(candidate.memberName)
	}
}

func (mm *Matcher) finalizeCandidate(ctx context.Context, candidate *matchCandidate) (*domain.Channel, error) {
	if candidate == nil {
		return nil, nil
	}

	if candidate.channelID == "" {
		mm.logger.Warn("Match candidate missing channel ID",
			slog.String("member", candidate.memberName),
			slog.String("source", candidate.source),
		)

		return nil, nil
	}

	channel, err := mm.hydrateChannel(ctx, candidate)
	if err != nil {
		return nil, err
	}

	if channel != nil {
		mm.logger.Debug("Match candidate resolved",
			slog.String("channel_id", candidate.channelID),
			slog.String("member", candidate.memberName),
			slog.String("source", candidate.source),
		)
	}

	return channel, nil
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
