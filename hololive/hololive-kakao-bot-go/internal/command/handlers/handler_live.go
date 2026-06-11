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

package handlers

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
)

type LiveCommand struct {
	BaseCommand
}

func NewLiveCommand(deps *Dependencies) *LiveCommand {
	return &LiveCommand{BaseCommand: NewBaseCommand(deps)}
}

func (c *LiveCommand) Name() string {
	return "live"
}

func (c *LiveCommand) Description() string {
	return "현재 방송 중인 스트림 목록"
}

// 특정 멤버 이름이 파라미터로 주어진 경우, 해당 멤버의 방송만 필터링한다.
func (c *LiveCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	memberName, hasMember := params["member"].(string)
	if hasMember && memberName != "" {
		return c.executeMemberLive(ctx, cmdCtx, memberName)
	}

	return c.executeAllLive(ctx, cmdCtx)
}

func (c *LiveCommand) executeMemberLive(ctx context.Context, cmdCtx *domain.CommandContext, memberName string) error {
	channel, err := FindActiveMemberWithCandidatesOrError(ctx, c.Deps(), cmdCtx.Room, memberName)
	if err != nil {
		return fmt.Errorf("failed to find member %q: %w", memberName, err)
	}
	if channel == nil {
		return nil
	}

	streams, err := c.Deps().Holodex.GetLiveStreams(ctx)
	if err != nil {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrLiveStreamQueryFailed)
	}

	memberStreams := filterLiveStreamsByChannel(streams, channel.ID)
	if len(memberStreams) == 0 {
		memberStreams = c.memberChzzkLiveStreams(ctx, channel.ID)
	}
	if len(memberStreams) == 0 {
		return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatMemberNotLive(channel.Name))
	}

	message := c.Deps().Formatter.FormatLiveStreams(ctx, memberStreams)
	return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
}

func filterLiveStreamsByChannel(streams []*domain.Stream, channelID string) []*domain.Stream {
	memberStreams := make([]*domain.Stream, 0, len(streams))
	for _, stream := range streams {
		if stream.ChannelID == channelID {
			memberStreams = append(memberStreams, stream)
		}
	}
	return memberStreams
}

func (c *LiveCommand) memberChzzkLiveStreams(ctx context.Context, channelID string) []*domain.Stream {
	member := c.Deps().Matcher.GetMemberByChannelID(ctx, channelID)
	if member == nil || member.ChzzkChannelID == "" || c.Deps().Chzzk == nil {
		return nil
	}
	chzzkStream := c.checkChzzkLive(ctx, member)
	if chzzkStream == nil {
		return nil
	}
	return []*domain.Stream{chzzkStream}
}

func (c *LiveCommand) executeAllLive(ctx context.Context, cmdCtx *domain.CommandContext) error {
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
	if member.ChzzkChannelID == "" || c.Deps().Chzzk == nil || !c.Deps().Chzzk.HasOpenAPICredentials() {
		return nil
	}

	lives, err := c.Deps().Chzzk.GetLivesByChannelIDs(ctx, []string{member.ChzzkChannelID})
	if err != nil {
		return nil
	}

	streams := buildChzzkLiveStreams([]*domain.Member{member}, lives)
	if len(streams) == 0 {
		return nil
	}

	return streams[0]
}

// getAllChzzkLiveStreams: Chzzk ID를 가진 모든 멤버의 방송 상태를 확인합니다.
func (c *LiveCommand) getAllChzzkLiveStreams(ctx context.Context) []*domain.Stream {
	if c.Deps().Chzzk == nil || c.Deps().MembersData == nil {
		return nil
	}

	if !c.Deps().Chzzk.HasOpenAPICredentials() {
		return nil
	}

	provider := c.Deps().MembersData.WithContext(ctx)
	if provider == nil {
		return nil
	}

	members := provider.GetAllMembers()

	return collectChzzkLiveStreams(
		members,
		func(channelIDs []string) ([]chzzk.LiveData, error) {
			return c.Deps().Chzzk.GetLivesByChannelIDs(ctx, channelIDs)
		},
	)
}

func buildChzzkLiveStreams(members []*domain.Member, lives []chzzk.LiveData) []*domain.Stream {
	if len(members) == 0 || len(lives) == 0 {
		return nil
	}

	byChzzkChannelID := buildLiveMemberByChzzkChannelID(members)

	streams := make([]*domain.Stream, 0, len(lives))
	for i := range lives {
		member, ok := byChzzkChannelID[lives[i].ChannelID]
		if !ok {
			continue
		}

		streams = append(streams, newChzzkStream(member, lives[i].LiveTitle))
	}

	return streams
}

func buildLiveMemberByChzzkChannelID(members []*domain.Member) map[string]*domain.Member {
	byChzzkChannelID := make(map[string]*domain.Member, len(members))
	for _, member := range members {
		if isEligibleChzzkLiveMember(member) {
			byChzzkChannelID[member.ChzzkChannelID] = member
		}
	}
	return byChzzkChannelID
}

func isEligibleChzzkLiveMember(member *domain.Member) bool {
	return member != nil && member.ChzzkChannelID != "" && !member.IsGraduated
}

func collectChzzkLiveStreams(
	members []*domain.Member,
	fetchBatch func([]string) ([]chzzk.LiveData, error),
) []*domain.Stream {
	eligibleMembers := make([]*domain.Member, 0, len(members))

	channelIDs := make([]string, 0, len(members))
	for _, member := range members {
		if member == nil || member.ChzzkChannelID == "" || member.IsGraduated {
			continue
		}

		eligibleMembers = append(eligibleMembers, member)
		channelIDs = append(channelIDs, member.ChzzkChannelID)
	}

	if len(eligibleMembers) == 0 {
		return nil
	}

	if fetchBatch == nil {
		return nil
	}

	lives, err := fetchBatch(channelIDs)
	if err != nil {
		return nil
	}

	return buildChzzkLiveStreams(eligibleMembers, lives)
}

func newChzzkStream(member *domain.Member, title string) *domain.Stream {
	if member == nil || strings.TrimSpace(member.ChzzkChannelID) == "" {
		return nil
	}

	title = strings.TrimSpace(title)
	if title == "" {
		title = "치지직 라이브"
	}

	now := time.Now().UTC().Truncate(time.Minute)
	liveURL := member.GetChzzkLiveURL()
	link := liveURL
	org := member.GetOrg()

	return &domain.Stream{
		ID:             buildChzzkDisplayStreamID(member.ChzzkChannelID, "live", title),
		Title:          title,
		ChannelID:      member.ChannelID,
		ChannelName:    member.Name,
		Status:         domain.StreamStatusLive,
		StartScheduled: &now,
		StartActual:    &now,
		Link:           &link,
		Channel: &domain.Channel{
			ID:   member.ChannelID,
			Name: member.Name,
			Org:  &org,
		},
		ChzzkChannelID: member.ChzzkChannelID,
		ChzzkLiveURL:   liveURL,
		IsChzzkOnly:    true,
	}
}

func buildChzzkDisplayStreamID(chzzkChannelID, kind, seed string) string {
	chzzkChannelID = strings.TrimSpace(chzzkChannelID)
	kind = strings.TrimSpace(kind)
	seed = strings.TrimSpace(seed)
	if kind == "" {
		kind = "unknown"
	}
	if seed == "" {
		seed = kind
	}

	sum := sha256.Sum256([]byte(chzzkChannelID + "|" + kind + "|" + seed))
	return fmt.Sprintf("chzzk:%s:%s:%x", chzzkChannelID, kind, sum[:8])
}

func (c *LiveCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil || c.Deps().Holodex == nil || c.Deps().Formatter == nil {
		return errors.New("live command services not configured")
	}

	return nil
}
