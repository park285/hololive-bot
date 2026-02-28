package command

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// LiveCommand: 현재 방송 중인 라이브 스트림 목록을 조회하고 보여주는 명령어
type LiveCommand struct {
	BaseCommand
}

// NewLiveCommand: LiveCommand 인스턴스를 생성합니다.
func NewLiveCommand(deps *Dependencies) *LiveCommand {
	return &LiveCommand{BaseCommand: NewBaseCommand(deps)}
}

// Name: 명령어의 고유 식별자('live')를 반환합니다.
func (c *LiveCommand) Name() string {
	return "live"
}

// Description: 명령어에 대한 사용자용 설명('현재 방송 중인 스트림 목록')을 반환합니다.
func (c *LiveCommand) Description() string {
	return "현재 방송 중인 스트림 목록"
}

// Execute: Holodex API와 Chzzk API를 통해 현재 진행 중인 방송을 조회하고, 결과를 포맷팅하여 채팅방에 전송합니다.
// 특정 멤버 이름이 파라미터로 주어진 경우, 해당 멤버의 방송만 필터링한다.
func (c *LiveCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	memberName, hasMember := params["member"].(string)

	if hasMember && memberName != "" {
		channel, err := FindActiveMemberOrError(ctx, c.Deps(), cmdCtx.Room, memberName)
		if err != nil {
			return fmt.Errorf("failed to find member %q: %w", memberName, err)
		}

		streams, err := c.Deps().Holodex.GetLiveStreams(ctx)
		if err != nil {
			return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrLiveStreamQueryFailed)
		}

		memberStreams := make([]*domain.Stream, 0, len(streams))
		for _, stream := range streams {
			if stream.ChannelID == channel.ID {
				memberStreams = append(memberStreams, stream)
			}
		}

		if len(memberStreams) == 0 {
			member := c.Deps().Matcher.GetMemberByChannelID(ctx, channel.ID)
			if member != nil && member.ChzzkChannelID != "" && c.Deps().Chzzk != nil {
				chzzkStream := c.checkChzzkLive(ctx, member)
				if chzzkStream != nil {
					memberStreams = append(memberStreams, chzzkStream)
				}
			}
		}

		if len(memberStreams) == 0 {
			return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMemberNotLive(channel.Name))
		}

		message := c.Deps().Formatter.FormatLiveStreams(ctx, memberStreams)
		return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
	}

	streams, err := c.Deps().Holodex.GetLiveStreams(ctx)
	if err != nil {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrLiveStreamQueryFailed)
	}

	chzzkStreams := c.getAllChzzkLiveStreams(ctx)
	streams = append(streams, chzzkStreams...)

	total := len(streams)
	if total > 10 {
		streams = streams[:10]
	}

	message := c.Deps().Formatter.FormatLiveStreams(ctx, streams)
	if total > 10 {
		message += c.Deps().Formatter.FormatLiveOverflowCount(total - 10)
	}
	return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
}

// checkChzzkLive: 특정 멤버의 Chzzk 방송 상태를 확인합니다.
func (c *LiveCommand) checkChzzkLive(ctx context.Context, member *domain.Member) *domain.Stream {
	if member.ChzzkChannelID == "" || c.Deps().Chzzk == nil {
		return nil
	}

	liveStatus, err := c.Deps().Chzzk.GetLiveStatus(ctx, member.ChzzkChannelID)
	if err != nil {
		return nil
	}

	if liveStatus == nil || liveStatus.Status != "OPEN" {
		return nil
	}

	return &domain.Stream{
		Title:          liveStatus.LiveTitle,
		ChannelID:      member.ChannelID,
		ChannelName:    member.Name,
		Status:         domain.StreamStatusLive,
		ChzzkChannelID: member.ChzzkChannelID,
		ChzzkLiveURL:   fmt.Sprintf("https://chzzk.naver.com/live/%s", member.ChzzkChannelID),
		IsChzzkOnly:    true,
	}
}

// getAllChzzkLiveStreams: Chzzk ID를 가진 모든 멤버의 방송 상태를 확인합니다.
func (c *LiveCommand) getAllChzzkLiveStreams(ctx context.Context) []*domain.Stream {
	if c.Deps().Chzzk == nil || c.Deps().MembersData == nil {
		return nil
	}

	members := c.Deps().MembersData.GetAllMembers()
	var streams []*domain.Stream

	for _, member := range members {
		if member.ChzzkChannelID == "" || member.IsGraduated {
			continue
		}

		stream := c.checkChzzkLive(ctx, member)
		if stream != nil {
			streams = append(streams, stream)
		}
	}

	return streams
}

func (c *LiveCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil || c.Deps().Holodex == nil || c.Deps().Formatter == nil {
		return fmt.Errorf("live command services not configured")
	}

	return nil
}
