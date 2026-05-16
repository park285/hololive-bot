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
	"fmt"
	"maps"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (h *StreamHandler) GetActiveMemberIndex(ctx context.Context) ([]string, map[string]string, error) {
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

	snapshot, ok := value.(memberIndexSnapshot)
	if !ok {
		return nil, nil, fmt.Errorf("member index snapshot: unexpected type")
	}

	return snapshot.channelIDs, snapshot.channelNames, nil
}

func (h *StreamHandler) refreshActiveMemberIndexSnapshot(ctx context.Context, state *StreamState) (memberIndexSnapshot, error) {
	if snapshot, ok := state.cachedMemberIndexSnapshot(time.Now()); ok {
		return snapshot, nil
	}

	members, err := h.fetchAllMembers(ctx)
	if err != nil {
		return memberIndexSnapshot{}, err
	}

	channelIDs, channelToName := BuildActiveMemberIndex(members)
	state.storeMemberIndexSnapshot(channelIDs, channelToName)

	return memberIndexSnapshot{channelIDs: channelIDs, channelNames: channelToName}, nil
}

func (s *StreamState) cachedMemberIndexSnapshot(now time.Time) (memberIndexSnapshot, bool) {
	s.memberIndexMu.RLock()
	defer s.memberIndexMu.RUnlock()

	if !s.memberIndexReady || !now.Before(s.memberIndexExpiresAt) {
		return memberIndexSnapshot{}, false
	}

	return memberIndexSnapshot{
		channelIDs:   append([]string(nil), s.memberChannelIDs...),
		channelNames: maps.Clone(s.memberChannelName),
	}, true
}

func (s *StreamState) storeMemberIndexSnapshot(channelIDs []string, channelToName map[string]string) {
	s.memberIndexMu.Lock()
	defer s.memberIndexMu.Unlock()

	s.memberChannelIDs = append([]string(nil), channelIDs...)
	s.memberChannelName = maps.Clone(channelToName)
	s.memberIndexExpiresAt = time.Now().Add(MemberIndexCacheTTL)
	s.memberIndexReady = true
}

func (h *StreamHandler) fetchAllMembers(ctx context.Context) ([]*domain.Member, error) {
	if h.MemberIndexLoader == nil {
		return nil, fmt.Errorf("load members: repository loader is nil")
	}

	members, err := h.MemberIndexLoader(ctx)
	if err != nil {
		return nil, fmt.Errorf("load members: get all members: %w", err)
	}

	return members, nil
}

func BuildActiveMemberIndex(members []*domain.Member) ([]string, map[string]string) {
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
