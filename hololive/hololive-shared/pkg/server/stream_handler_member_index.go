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

package server

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (h *StreamHandler) GetActiveMemberIndex(ctx context.Context) ([]string, map[string]string, error) {
	state := h.ensureState()
	now := time.Now()

	state.memberIndexMu.RLock()
	if state.memberIndexReady && now.Before(state.memberIndexExpiresAt) {
		channelIDs := append([]string(nil), state.memberChannelIDs...)
		channelToName := maps.Clone(state.memberChannelName)
		state.memberIndexMu.RUnlock()
		return channelIDs, channelToName, nil
	}
	state.memberIndexMu.RUnlock()

	value, err, _ := state.memberIndexBuildGroup.Do("refresh", func() (any, error) {
		state.memberIndexMu.RLock()
		if state.memberIndexReady && time.Now().Before(state.memberIndexExpiresAt) {
			channelIDs := append([]string(nil), state.memberChannelIDs...)
			channelToName := maps.Clone(state.memberChannelName)
			state.memberIndexMu.RUnlock()
			return memberIndexSnapshot{channelIDs: channelIDs, channelNames: channelToName}, nil
		}
		state.memberIndexMu.RUnlock()

		members, loadErr := h.fetchAllMembers(ctx)
		if loadErr != nil {
			return nil, loadErr
		}

		channelIDs, channelToName := BuildActiveMemberIndex(members)

		state.memberIndexMu.Lock()
		state.memberChannelIDs = append([]string(nil), channelIDs...)
		state.memberChannelName = maps.Clone(channelToName)
		state.memberIndexExpiresAt = time.Now().Add(MemberIndexCacheTTL)
		state.memberIndexReady = true
		state.memberIndexMu.Unlock()

		return memberIndexSnapshot{channelIDs: channelIDs, channelNames: channelToName}, nil
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
