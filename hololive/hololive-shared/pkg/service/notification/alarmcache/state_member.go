package alarmcache

import (
	"context"
	"fmt"

	"github.com/park285/shared-go/v2/pkg/stringutil"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

func (s *State) CacheMemberName(ctx context.Context, channelID, memberName string) error {
	if err := s.Cache.HSet(ctx, sharedalarmkeys.MemberNameKey, channelID, memberName); err != nil {
		return fmt.Errorf("cache member name: %w", err)
	}

	return nil
}

func (s *State) GetMemberName(ctx context.Context, channelID string) (string, error) {
	name, err := s.Cache.HGet(ctx, sharedalarmkeys.MemberNameKey, channelID)
	if err != nil {
		return "", fmt.Errorf("get member name: %w", err)
	}

	return name, nil
}

func (s *State) ResolveCacheMemberName(ctx context.Context, channelID, fallback string) string {
	if name := s.ResolveMemberDataName(ctx, channelID); name != "" {
		return name
	}

	return stringutil.TrimSpace(fallback)
}

func (s *State) ResolveMemberDataName(ctx context.Context, channelID string) string {
	provider := s.memberData()
	if provider == nil {
		return ""
	}

	if scoped := provider.WithContext(ctx); scoped != nil {
		provider = scoped
	}

	member := provider.FindMemberByChannelID(channelID)
	if member == nil {
		return ""
	}

	return FirstMemberName(member.ShortKoreanName, member.NameKo, member.Name)
}

func FirstMemberName(candidates ...string) string {
	for _, candidate := range candidates {
		if name := stringutil.TrimSpace(candidate); name != "" {
			return name
		}
	}

	return ""
}

func (s *State) GetChannelSubscribersByType(ctx context.Context, channelID string, alarmType domain.AlarmType) ([]string, error) {
	subscribers, err := sharedalarm.LookupChannelSubscribersByType(ctx, s.Cache, channelID, alarmType)
	if err != nil {
		return nil, fmt.Errorf("lookup channel subscribers by type: %w", err)
	}

	return subscribers, nil
}

func (s *State) SetRoomName(ctx context.Context, roomID, roomName string) error {
	if err := s.Cache.HSet(ctx, sharedalarmkeys.RoomNamesCacheKey, roomID, roomName); err != nil {
		return fmt.Errorf("set room name: %w", err)
	}

	return nil
}

func (s *State) SetUserName(ctx context.Context, userID, userName string) error {
	if err := s.Cache.HSet(ctx, sharedalarmkeys.UserNamesCacheKey, userID, userName); err != nil {
		return fmt.Errorf("set user name: %w", err)
	}

	return nil
}

func (s *State) GetMemberNamesBatch(ctx context.Context, channelIDs []string) (map[string]string, error) {
	if len(channelIDs) == 0 {
		return map[string]string{}, nil
	}

	out, err := s.Cache.BatchHGet(ctx, sharedalarmkeys.MemberNameKey, channelIDs)
	if err != nil {
		return nil, fmt.Errorf("batch h get: %w", err)
	}

	return out, nil
}
