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

package settings

import (
	"slices"
	"sync"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

const aclModeBlacklist = "blacklist"

type KakaoConfig struct {
	Rooms      []string
	ACLEnabled bool
	ACLMode    string // "whitelist" 또는 "blacklist"

	mu sync.RWMutex
}

// Thread-safe하게 읽기 락을 사용한다.
func (c *KakaoConfig) SnapshotACL() (enabled bool, mode string, rooms []string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rooms = append([]string(nil), c.Rooms...)
	return c.ACLEnabled, c.ACLMode, rooms
}

func (c *KakaoConfig) SetACLEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ACLEnabled = enabled
}

func (c *KakaoConfig) AddRoom(room string) bool {
	room = stringutil.TrimSpace(room)
	if room == "" {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if slices.Contains(c.Rooms, room) {
		return false
	}

	c.Rooms = append(c.Rooms, room)
	return true
}

func (c *KakaoConfig) RemoveRoom(room string) bool {
	room = stringutil.TrimSpace(room)
	if room == "" {
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	removed := false
	rooms := make([]string, 0, len(c.Rooms))
	for _, existing := range c.Rooms {
		if existing == room {
			removed = true
			continue
		}
		rooms = append(rooms, existing)
	}

	c.Rooms = rooms
	return removed
}

// ACL이 비활성화되어 있으면 모든 방을 허용한다.
func (c *KakaoConfig) IsRoomAllowed(roomName, chatID string) bool {
	chatID = stringutil.TrimSpace(chatID)

	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.ACLEnabled {
		return true
	}

	// chatID 기반으로만 검증 (roomName은 참고용으로만 유지)
	if chatID == "" {
		return false // chatID가 없으면 거부
	}

	inList := slices.Contains(c.Rooms, chatID)

	switch stringutil.Normalize(c.ACLMode) {
	case aclModeBlacklist:
		// 블랙리스트: 목록에 있으면 차단
		return !inList
	default:
		// 화이트리스트: 목록에 있으면 허용
		return inList
	}
}
