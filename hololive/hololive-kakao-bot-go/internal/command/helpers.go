package command

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// FindMemberOrError: 멤버 이름으로 채널을 검색하고, 찾지 못한 경우 에러 메시지를 전송합니다.
// 성공 시 (*domain.Channel, nil)을, 실패 시 (nil, error)를 반환한다.
func FindMemberOrError(ctx context.Context, deps *Dependencies, room, memberName string) (*domain.Channel, error) {
	if deps == nil || deps.Matcher == nil {
		return nil, deps.SendError(ctx, room, adapter.ErrMatcherNotActivated)
	}

	member, err := deps.Matcher.FindBestMatch(ctx, memberName)
	if err != nil {
		return nil, deps.SendError(ctx, room, deps.Formatter.MemberNotFound(memberName))
	}

	if member == nil {
		return nil, deps.SendError(ctx, room, deps.Formatter.MemberNotFound(memberName))
	}

	return member, nil
}

// FindActiveMemberOrError: 멤버 이름으로 채널을 검색하고, 졸업 멤버는 차단합니다.
// !라이브, !일정, !알람 명령에서 사용한다.
// 성공 시 (*domain.Channel, nil)을, 실패 또는 졸업 멤버인 경우 (nil, error)를 반환한다.
func FindActiveMemberOrError(ctx context.Context, deps *Dependencies, room, memberName string) (*domain.Channel, error) {
	channel, err := FindMemberOrError(ctx, deps, room, memberName)
	if err != nil {
		return nil, err
	}

	// Matcher를 통해 Member 정보 조회하여 졸업 상태 확인
	if deps.Matcher != nil {
		if member := deps.Matcher.GetMemberByChannelID(ctx, channel.ID); member != nil && member.IsGraduated {
			return nil, deps.SendError(ctx, room, adapter.ErrGraduatedMemberBlocked)
		}
	}

	return channel, nil
}
