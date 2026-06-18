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

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
)

var ErrMemberLookupHandled = errors.New("member lookup handled")

// 성공 시 (*domain.Channel, nil)을, 사용자-facing 응답을 보낸 경우 ErrMemberLookupHandled를 반환한다.
func FindMemberOrError(ctx context.Context, deps *Dependencies, room, memberName string) (*domain.Channel, error) {
	if err := ValidateMemberLookupDependencies(deps); err != nil {
		return nil, fmt.Errorf("member lookup dependencies not configured: %w", err)
	}

	member, err := deps.Matcher.FindBestMatch(ctx, memberName)
	if err != nil {
		return nil, sendMemberNotFound(ctx, deps, room, memberName)
	}

	if member == nil {
		return nil, sendMemberNotFound(ctx, deps, room, memberName)
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
	if deps.Matcher != nil {
		if member := deps.Matcher.GetMemberByChannelID(ctx, channel.ID); member != nil && member.IsGraduated {
			return nil, sendGraduatedMemberBlocked(ctx, deps, room)
		}
	}

	return channel, nil
}

// 동명이인 또는 미발견인 경우 사용자-facing 응답을 보내고 ErrMemberLookupHandled를 반환한다.
func FindMemberWithCandidatesOrError(ctx context.Context, deps *Dependencies, room, memberName string) (*domain.Channel, error) {
	if err := ValidateMemberLookupDependencies(deps); err != nil {
		return nil, fmt.Errorf("member lookup dependencies not configured: %w", err)
	}

	channel, err := deps.Matcher.FindBestMatchWithCandidates(ctx, memberName)
	if err != nil {
		var ambiguousErr *matcher.AmbiguousMatchError

		if errors.As(err, &ambiguousErr) {
			message := deps.Formatter.FormatAmbiguousMembers(ambiguousErr.Candidates)

			return nil, sendAmbiguousMembers(ctx, deps, room, message)
		}

		return nil, sendMemberNotFound(ctx, deps, room, memberName)
	}

	if channel == nil {
		return nil, sendMemberNotFound(ctx, deps, room, memberName)
	}

	return channel, nil
}

// !라이브, !일정, !예정 명령에서 사용한다. 동명이인 응답과 졸업 멤버 차단을 함께 처리한다.
func FindActiveMemberWithCandidatesOrError(ctx context.Context, deps *Dependencies, room, memberName string) (*domain.Channel, error) {
	channel, err := FindMemberWithCandidatesOrError(ctx, deps, room, memberName)
	if err != nil {
		return nil, fmt.Errorf("find member with candidates: %w", err)
	}

	if channel == nil {
		return nil, ErrMemberLookupHandled
	}

	if deps.Matcher != nil {
		if member := deps.Matcher.GetMemberByChannelID(ctx, channel.ID); member != nil && member.IsGraduated {
			return nil, sendGraduatedMemberBlocked(ctx, deps, room)
		}
	}

	return channel, nil
}

func sendMemberNotFound(ctx context.Context, deps *Dependencies, room, memberName string) error {
	if err := deps.SendError(ctx, room, deps.Formatter.MemberNotFound(memberName)); err != nil {
		return fmt.Errorf("send member not found response: %w", err)
	}
	return ErrMemberLookupHandled
}

func sendGraduatedMemberBlocked(ctx context.Context, deps *Dependencies, room string) error {
	if err := deps.SendError(ctx, room, adapter.ErrGraduatedMemberBlocked); err != nil {
		return fmt.Errorf("send graduated member blocked response: %w", err)
	}
	return ErrMemberLookupHandled
}

func sendAmbiguousMembers(ctx context.Context, deps *Dependencies, room, message string) error {
	if err := deps.SendMessage(ctx, room, message); err != nil {
		return fmt.Errorf("send ambiguous members response: %w", err)
	}
	return ErrMemberLookupHandled
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
