package info

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type MemberInfoCommand struct {
	handlercore.BaseCommand
}

func NewMemberInfoCommand(deps *handlercore.Dependencies) *MemberInfoCommand {
	return &MemberInfoCommand{BaseCommand: handlercore.NewBaseCommand(deps)}
}

func (c *MemberInfoCommand) Name() string {
	return string(botcommand.CommandMemberInfo)
}

func (c *MemberInfoCommand) Description() string {
	return "등록된 멤버 기본 정보"
}

func (c *MemberInfoCommand) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params botcommand.MemberInfoParams) error {
	rawQuery := strings.TrimSpace(params.Query)

	if hasNoMemberInfoQuery(rawQuery) {
		if err := c.renderMemberDirectory(ctx, cmdCtx); err != nil {
			return fmt.Errorf("render member directory: %w", err)
		}

		return nil
	}

	member, err := c.resolveRequestedMember(ctx, cmdCtx.Room, rawQuery)
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

func (c *MemberInfoCommand) resolveRequestedMember(ctx context.Context, room, rawQuery string) (*domain.Member, error) {
	member, err := c.resolveMember(ctx, rawQuery)
	if ambiguous, ok := errors.AsType[*matcher.AmbiguousMatchError](err); ok {
		message := c.Deps().Formatter.FormatAmbiguousMembers(ctx, ambiguous.Candidates, "정보")
		if sendErr := c.Deps().SendMessage(ctx, room, message); sendErr != nil {
			return nil, fmt.Errorf("send ambiguous member information: %w", sendErr)
		}

		return nil, handlercore.ErrMemberLookupHandled
	}

	if err != nil && !errors.Is(err, domain.ErrMemberNotFound) {
		return nil, fmt.Errorf("load requested member: %w", err)
	}

	if member != nil {
		return member, nil
	}

	if err := c.sendMemberNotFound(ctx, room, rawQuery); err != nil {
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

func hasNoMemberInfoQuery(rawQuery string) bool {
	return strings.TrimSpace(rawQuery) == ""
}

func (c *MemberInfoCommand) sendMemberNotFound(ctx context.Context, room, rawQuery string) error {
	if err := c.Deps().SendMessage(ctx, room, c.Deps().Formatter.MemberNotFound(ctx, rawQuery)); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (c *MemberInfoCommand) CheckDependencies() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("ensure base dependencies: %w", err)
	}

	if c.Deps().MembersData == nil ||
		c.Deps().Formatter == nil {
		return errors.New("member info command services not configured")
	}

	return nil
}

func (c *MemberInfoCommand) resolveMember(ctx context.Context, query string) (*domain.Member, error) {
	members, err := c.Deps().MembersData.LoadAllMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("load member information snapshot: %w", err)
	}

	query = strings.TrimSpace(query)

	candidates := memberInfoCandidates(members, query)
	if len(candidates) > 1 {
		return nil, matcher.NewAmbiguousMatchError(query, candidates)
	}

	if len(candidates) == 1 {
		return candidates[0], nil
	}

	return nil, domain.ErrMemberNotFound
}

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
	return strings.Join(strings.Fields(matcher.NormalizeSuffix(value)), " ")
}
