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

package httpserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// 반환된 slice/map은 캐시가 보유한 불변 스냅샷을 그대로 공유한다. 호출자는 수정하면 안 된다.
func (h *StreamHandler) GetActiveMemberIndex(ctx context.Context) (result0 []string, result1 map[string]string, err error) {
	state := h.ensureState()
	if snapshot, ok := state.cachedMemberIndexSnapshot(time.Now()); ok {
		return snapshot.channelIDs, snapshot.channelNames, nil
	}

	value, err, _ := state.memberIndexBuildGroup.Do("refresh", func() (any, error) {
		return h.refreshActiveMemberIndexSnapshot(ctx, state)
	})
	if err != nil {
		return nil, nil, fmt.Errorf("member index singleflight: %w", err)
	}

	snapshot, ok := value.(*memberIndexSnapshot)
	if !ok || snapshot == nil {
		return nil, nil, errors.New("member index snapshot: unexpected type")
	}

	return snapshot.channelIDs, snapshot.channelNames, nil
}

func (h *StreamHandler) refreshActiveMemberIndexSnapshot(ctx context.Context, state *StreamState) (*memberIndexSnapshot, error) {
	if snapshot, ok := state.cachedMemberIndexSnapshot(time.Now()); ok {
		return snapshot, nil
	}

	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), MemberIndexRefreshTimeout)
	defer cancel()

	members, err := h.fetchAllMembers(refreshCtx)
	if err != nil {
		return nil, fmt.Errorf("fetch all members: %w", err)
	}

	channelIDs, channelToName := BuildActiveMemberIndex(members)

	return state.storeMemberIndexSnapshot(channelIDs, channelToName), nil
}

func (s *StreamState) cachedMemberIndexSnapshot(now time.Time) (*memberIndexSnapshot, bool) {
	snapshot := s.memberIndex.Load()
	if snapshot == nil || !now.Before(snapshot.expiresAt) {
		return nil, false
	}

	return snapshot, true
}

func (s *StreamState) storeMemberIndexSnapshot(channelIDs []string, channelToName map[string]string) *memberIndexSnapshot {
	snapshot := &memberIndexSnapshot{
		channelIDs:   channelIDs,
		channelNames: channelToName,
		expiresAt:    time.Now().Add(MemberIndexCacheTTL),
	}
	s.memberIndex.Store(snapshot)

	return snapshot
}

func (h *StreamHandler) fetchAllMembers(ctx context.Context) ([]*domain.Member, error) {
	if h.MemberIndexLoader == nil {
		return nil, errors.New("load members: repository loader is nil")
	}

	members, err := h.MemberIndexLoader(ctx)
	if err != nil {
		return nil, fmt.Errorf("load members: get all members: %w", err)
	}

	return members, nil
}

func BuildActiveMemberIndex(members []*domain.Member) (result0 []string, result1 map[string]string) {
	channelIDs := make([]string, 0, len(members))
	channelToName := make(map[string]string, len(members))

	for _, member := range members {
		if member.ChannelID == "" || member.IsGraduated {
			continue
		}

		channelIDs = append(channelIDs, member.ChannelID)
		channelToName[member.ChannelID] = member.Name
	}

	return channelIDs, channelToName
}
