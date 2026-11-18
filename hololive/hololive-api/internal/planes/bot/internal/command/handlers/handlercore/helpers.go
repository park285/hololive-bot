package handlercore

import (
	"context"
	"errors"
	"fmt"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var ErrMemberLookupHandled = errors.New("member lookup handled")

func BlockGraduatedMember(ctx context.Context, deps *Dependencies, room, channelID string) (bool, error) {
	if deps.Matcher == nil {
		return false, nil
	}

	member, err := deps.Matcher.GetMemberByChannelID(ctx, channelID)
	if errors.Is(err, domain.ErrMemberNotFound) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("get member by channel ID: %w", err)
	}

	if !member.IsGraduated {
		return false, nil
	}

	if replyErr := sendGraduatedMemberBlocked(ctx, deps, room); replyErr != nil {
		return false, fmt.Errorf("send graduated member blocked: %w", replyErr)
	}

	return true, nil
}

func FindMemberWithCandidatesOrError(ctx context.Context, deps *Dependencies, room, memberName, commandExample string) (*domain.Channel, error) {
	member, err := lookupMemberProfile(ctx, deps, room, memberName, commandExample)
	if err != nil {
		return nil, err
	}

	return &domain.Channel{ID: member.ChannelID, Name: member.DisplayName(), Org: &member.Org}, nil
}

func FindActiveMemberProfile(ctx context.Context, deps *Dependencies, room, memberName, commandExample string) (*domain.Member, error) {
	member, err := lookupMemberProfile(ctx, deps, room, memberName, commandExample)
	if err != nil {
		return nil, err
	}

	if member.IsGraduated {
		if err := sendGraduatedMemberBlocked(ctx, deps, room); err != nil {
			return nil, err
		}

		return nil, ErrMemberLookupHandled
	}

	return member, nil
}

func lookupMemberProfile(ctx context.Context, deps *Dependencies, room, memberName, commandExample string) (*domain.Member, error) {
	if err := ValidateMemberLookupDependencies(deps); err != nil {
		return nil, fmt.Errorf("member lookup dependencies not configured: %w", err)
	}

	member, found, err := deps.Matcher.FindMemberProfile(ctx, memberName)
	if err != nil {
		if replyErr := replyMemberLookupFailure(ctx, deps, room, commandExample, err); replyErr != nil {
			return nil, fmt.Errorf("reply member lookup failure: %w", replyErr)
		}

		return nil, ErrMemberLookupHandled
	}

	if !found {
		if replyErr := sendMemberNotFound(ctx, deps, room, memberName); replyErr != nil {
			return nil, fmt.Errorf("send member not found: %w", replyErr)
		}

		return nil, ErrMemberLookupHandled
	}

	if member == nil {
		return nil, errors.New("member matcher returned found without member")
	}

	return member, nil
}

func replyMemberLookupFailure(ctx context.Context, deps *Dependencies, room, commandExample string, lookupErr error) error {
	ambiguousErr, ok := errors.AsType[*matcher.AmbiguousMatchError](lookupErr)
	if !ok {
		return fmt.Errorf("find member candidates: %w", lookupErr)
	}

	message := deps.Formatter.FormatAmbiguousMembers(ctx, ambiguousErr.Candidates, commandExample)
	if err := sendAmbiguousMembers(ctx, deps, room, message); err != nil {
		return fmt.Errorf("send ambiguous members: %w", err)
	}

	return nil
}

func FindActiveMemberWithCandidatesOrError(ctx context.Context, deps *Dependencies, room, memberName, commandExample string) (*domain.Channel, error) {
	channel, err := FindMemberWithCandidatesOrError(ctx, deps, room, memberName, commandExample)
	if err != nil {
		return nil, fmt.Errorf("find member with candidates: %w", err)
	}

	blocked, err := BlockGraduatedMember(ctx, deps, room, channel.ID)
	if err != nil {
		return nil, fmt.Errorf("block graduated member: %w", err)
	}

	if blocked {
		return nil, ErrMemberLookupHandled
	}

	return channel, nil
}

func sendMemberNotFound(ctx context.Context, deps *Dependencies, room, memberName string) error {
	if err := deps.SendMessage(ctx, room, deps.Formatter.MemberNotFound(ctx, memberName)); err != nil {
		return fmt.Errorf("send member not found response: %w", err)
	}

	return nil
}

func sendGraduatedMemberBlocked(ctx context.Context, deps *Dependencies, room string) error {
	if err := deps.SendError(ctx, room, messaging.ErrGraduatedMemberBlocked); err != nil {
		return fmt.Errorf("send graduated member blocked response: %w", err)
	}

	return nil
}

func sendAmbiguousMembers(ctx context.Context, deps *Dependencies, room, message string) error {
	if err := deps.SendMessage(ctx, room, message); err != nil {
		return fmt.Errorf("send ambiguous members response: %w", err)
	}

	return nil
}

func ValidateMemberLookupDependencies(deps *Dependencies) error {
	if deps == nil {
		return errors.New("deps is nil")
	}

	if deps.Matcher == nil {
		return errors.New("matcher is nil")
	}

	if deps.Formatter == nil {
		return errors.New("formatter is nil")
	}

	if deps.SendError == nil {
		return errors.New("send error callback is nil")
	}

	return nil
}
