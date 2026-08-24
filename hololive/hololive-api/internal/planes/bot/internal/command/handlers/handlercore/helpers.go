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

package handlercore

import (
	"context"
	"errors"
	"fmt"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/domain"
)

var ErrMemberLookupHandled = errors.New("member lookup handled")

// 전송 결과가 unknown이면 이미 전달됐을 수 있으므로, 호출자는 대체 응답을 추가로 보내면 안 된다.
func IsReplyOutcomeUnknown(err error) bool {
	return errors.Is(err, transport.ErrReplyOutcomeUnknown)
}

// 성공 시 (*domain.Channel, nil)을, 사용자-facing 응답을 보낸 경우 ErrMemberLookupHandled를 반환한다.
func FindMemberOrError(ctx context.Context, deps *Dependencies, room, memberName string) (*domain.Channel, error) {
	if err := ValidateMemberLookupDependencies(deps); err != nil {
		return nil, fmt.Errorf("member lookup dependencies not configured: %w", err)
	}

	member, found, err := deps.Matcher.FindBestMatch(ctx, memberName)
	if err != nil || !found {
		if replyErr := sendMemberNotFound(ctx, deps, room, memberName); replyErr != nil {
			return nil, fmt.Errorf("send member not found: %w", replyErr)
		}

		return nil, ErrMemberLookupHandled
	}

	if member == nil {
		return nil, errors.New("member matcher returned found without channel")
	}

	return member, nil
}

// !라이브, !일정, !알람 명령에서 사용한다.
// 성공 시 (*domain.Channel, nil)을, 사용자-facing 응답을 보낸 경우 ErrMemberLookupHandled를 반환한다.
func FindActiveMemberOrError(ctx context.Context, deps *Dependencies, room, memberName string) (*domain.Channel, error) {
	channel, err := FindMemberOrError(ctx, deps, room, memberName)
	if err != nil {
		return nil, fmt.Errorf("find member: %w", err)
	}

	// Matcher를 통해 Member 정보 조회하여 졸업 상태 확인
	blocked, err := blockGraduatedMember(ctx, deps, room, channel.ID)
	if err != nil {
		return nil, fmt.Errorf("block graduated member: %w", err)
	}

	if blocked {
		return nil, ErrMemberLookupHandled
	}

	return channel, nil
}

func blockGraduatedMember(ctx context.Context, deps *Dependencies, room, channelID string) (bool, error) {
	if deps.Matcher == nil {
		return false, nil
	}

	member := deps.Matcher.GetMemberByChannelID(ctx, channelID)
	if member == nil || !member.IsGraduated {
		return false, nil
	}

	if replyErr := sendGraduatedMemberBlocked(ctx, deps, room); replyErr != nil {
		return false, fmt.Errorf("send graduated member blocked: %w", replyErr)
	}

	return true, nil
}

// 동명이인 또는 미발견인 경우 사용자-facing 응답을 보내고 ErrMemberLookupHandled를 반환한다.
func FindMemberWithCandidatesOrError(ctx context.Context, deps *Dependencies, room, memberName, commandExample string) (*domain.Channel, error) {
	if err := ValidateMemberLookupDependencies(deps); err != nil {
		return nil, fmt.Errorf("member lookup dependencies not configured: %w", err)
	}

	channel, found, err := deps.Matcher.FindBestMatchWithCandidates(ctx, memberName)
	if err != nil {
		if replyErr := replyMemberLookupFailure(ctx, deps, room, memberName, commandExample, err); replyErr != nil {
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

	return channel, nil
}

func replyMemberLookupFailure(ctx context.Context, deps *Dependencies, room, memberName, commandExample string, lookupErr error) error {
	ambiguousErr, ok := errors.AsType[*matcher.AmbiguousMatchError](lookupErr)
	if !ok {
		if err := sendMemberNotFound(ctx, deps, room, memberName); err != nil {
			return fmt.Errorf("send member not found: %w", err)
		}

		return nil
	}

	message := deps.Formatter.FormatAmbiguousMembers(ctx, ambiguousErr.Candidates, commandExample)
	if err := sendAmbiguousMembers(ctx, deps, room, message); err != nil {
		return fmt.Errorf("send ambiguous members: %w", err)
	}

	return nil
}

// !라이브, !일정, !예정 명령에서 사용한다. 동명이인 응답과 졸업 멤버 차단을 함께 처리한다.
func FindActiveMemberWithCandidatesOrError(ctx context.Context, deps *Dependencies, room, memberName, commandExample string) (*domain.Channel, error) {
	channel, err := FindMemberWithCandidatesOrError(ctx, deps, room, memberName, commandExample)
	if err != nil {
		return nil, fmt.Errorf("find member with candidates: %w", err)
	}

	if channel == nil {
		return nil, ErrMemberLookupHandled
	}

	blocked, err := blockGraduatedMember(ctx, deps, room, channel.ID)
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
