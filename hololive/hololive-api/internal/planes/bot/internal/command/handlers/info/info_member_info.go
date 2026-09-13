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

package info

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
	membersvc "github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/util"
)

type MemberInfoCommand struct {
	handlercore.BaseCommand
}

func NewMemberInfoCommand(deps *handlercore.Dependencies) *MemberInfoCommand {
	return &MemberInfoCommand{BaseCommand: handlercore.NewBaseCommand(deps)}
}

func (c *MemberInfoCommand) Name() string {
	return string(domain.CommandMemberInfo)
}

func (c *MemberInfoCommand) Description() string {
	return "등록된 멤버 기본 정보"
}

// 쿼리가 없으면 멤버 디렉터리를, 있으면 개별 기본 정보를 표시합니다.
func (c *MemberInfoCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	rawQuery := getStringParam(params, "query")
	englishCandidate := getStringParam(params, "member")
	channelID := getStringParam(params, "channel_id")

	if hasNoMemberInfoQuery(rawQuery, englishCandidate, channelID) {
		if err := c.renderMemberDirectory(ctx, cmdCtx); err != nil {
			return fmt.Errorf("render member directory: %w", err)
		}

		return nil
	}

	member, err := c.resolveRequestedMember(ctx, cmdCtx.Room, channelID, englishCandidate, rawQuery)
	if errors.Is(err, handlercore.ErrMemberLookupHandled) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("resolve requested member: %w", err)
	}

	if member == nil {
		return nil
	}

	if err := c.sendMemberProfile(ctx, cmdCtx.Room, member); err != nil {
		return fmt.Errorf("send member profile: %w", err)
	}

	return nil
}

func (c *MemberInfoCommand) resolveRequestedMember(ctx context.Context, room, channelID, englishCandidate, rawQuery string) (*domain.Member, error) {
	member, err := c.resolveMember(ctx, channelID, englishCandidate, rawQuery)
	if ambiguous, ok := errors.AsType[*matcher.AmbiguousMatchError](err); ok {
		message := c.Deps().Formatter.FormatAmbiguousMembers(ctx, ambiguous.Candidates, "정보")
		if sendErr := c.Deps().SendMessage(ctx, room, message); sendErr != nil {
			return nil, fmt.Errorf("send ambiguous member information: %w", sendErr)
		}

		return nil, handlercore.ErrMemberLookupHandled
	}

	if err != nil && !errors.Is(err, membersvc.ErrMemberNotFound) {
		return nil, fmt.Errorf("load requested member: %w", err)
	}

	if member != nil {
		return member, nil
	}

	if err := c.sendMemberNotFound(ctx, room, englishCandidate, rawQuery); err != nil {
		return nil, fmt.Errorf("send member not found: %w", err)
	}

	return nil, handlercore.ErrMemberLookupHandled
}

func (c *MemberInfoCommand) sendMemberProfile(ctx context.Context, room string, member *domain.Member) error {
	message := c.Deps().Formatter.FormatMemberInfo(ctx, member)
	if message == "" {
		if err := c.Deps().SendError(ctx, room, messaging.ErrMemberProfileBuildFailed); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
	}

	if member.IsGraduated {
		message = c.Deps().Formatter.GraduatedMemberWarning(ctx) + message
	}

	if err := c.Deps().SendMessage(ctx, room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func hasNoMemberInfoQuery(rawQuery, englishCandidate, channelID string) bool {
	return stringutil.TrimSpace(rawQuery) == "" &&
		stringutil.TrimSpace(englishCandidate) == "" &&
		stringutil.TrimSpace(channelID) == ""
}

func (c *MemberInfoCommand) sendMemberNotFound(ctx context.Context, room, englishCandidate, rawQuery string) error {
	target := englishCandidate
	if target == "" {
		target = rawQuery
	}

	if err := c.Deps().SendMessage(ctx, room, c.Deps().Formatter.MemberNotFound(ctx, target)); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (c *MemberInfoCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.Deps().MembersData == nil ||
		c.Deps().Formatter == nil {
		return errors.New("member info command services not configured")
	}

	return nil
}

func (c *MemberInfoCommand) resolveMember(ctx context.Context, channelID, englishName, query string) (*domain.Member, error) {
	members, err := domain.LoadAllMembers(c.Deps().MembersData.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("load member information snapshot: %w", err)
	}

	query = strings.TrimSpace(query)
	if query == "" {
		query = englishName
	}

	if query != "" {
		candidates := memberInfoCandidates(members, query)
		if len(candidates) > 1 {
			return nil, matcher.NewAmbiguousMatchError(query, candidates)
		}

		if len(candidates) == 1 {
			return candidates[0], nil
		}

		return nil, membersvc.ErrMemberNotFound
	}

	var representative *domain.Member

	for _, member := range members {
		if member != nil && member.ChannelID == channelID && (representative == nil || member.ID < representative.ID) {
			representative = member
		}
	}

	if representative == nil {
		return nil, membersvc.ErrMemberNotFound
	}

	return representative, nil
}

// 개인 이름 조회는 채널 매처의 중복 채널 병합을 거치지 않는다. 부분 일치도 개인 ID를 보존한다.
func memberInfoCandidates(members []*domain.Member, query string) []*domain.Member {
	name, org := matcher.ParseNameWithOrg(query)
	normalized := normalizeMemberInfoTerm(name)

	if normalized == "" {
		return nil
	}

	bestRank := 0

	var matches []*domain.Member

	for _, member := range members {
		if member == nil || (org != "" && !strings.EqualFold(member.Org, org)) {
			continue
		}

		rank := memberInfoMatchRank(member, normalized)
		if rank == 0 || rank < bestRank {
			continue
		}

		if rank > bestRank {
			matches = nil
			bestRank = rank
		}

		matches = append(matches, member)
	}

	return matches
}

func memberInfoMatchRank(member *domain.Member, query string) int {
	rank := 0

	for _, name := range []string{member.Name, member.NameKo, member.NameJa} {
		normalized := normalizeMemberInfoTerm(name)
		if normalized == query {
			return 4
		}

		if normalized != "" && strings.Contains(normalized, query) {
			rank = 2
		}
	}

	for _, alias := range member.GetAllAliases() {
		normalized := normalizeMemberInfoTerm(alias)
		if normalized == query {
			return 3
		}

		if rank == 0 && normalized != "" && strings.Contains(normalized, query) {
			rank = 1
		}
	}

	return rank
}

func normalizeMemberInfoTerm(value string) string {
	return strings.Join(strings.Fields(util.NormalizeSuffix(value)), " ")
}

func getStringParam(params map[string]any, key string) string {
	if params == nil {
		return ""
	}

	val, ok := params[key]
	if !ok {
		return ""
	}

	switch v := val.(type) {
	case string:
		return stringutil.TrimSpace(v)
	default:
		return stringutil.TrimSpace(fmt.Sprintf("%v", v))
	}
}
