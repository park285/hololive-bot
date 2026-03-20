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

package config

import (
	"slices"
	"strings"
	"sync"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
)

// KakaoConfig: 카카오톡 채팅방 접근 제어(ACL) 설정
type KakaoConfig struct {
	Rooms      []string
	ACLEnabled bool
	ACLMode    string // "whitelist" 또는 "blacklist"

	mu sync.RWMutex
}

// SnapshotACL: 현재 ACL 설정 상태(활성화 여부, 모드, 방 목록)의 스냅샷을 반환합니다.
// Thread-safe하게 읽기 락을 사용한다.
func (c *KakaoConfig) SnapshotACL() (enabled bool, mode string, rooms []string) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	rooms = append([]string(nil), c.Rooms...)
	return c.ACLEnabled, c.ACLMode, rooms
}

// SetACLEnabled: ACL(접근 제어) 기능의 활성화 여부를 '동적으로' 설정합니다.
func (c *KakaoConfig) SetACLEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ACLEnabled = enabled
}

// AddRoom: ACL 목록에 새로운 채팅방을 추가한다. 이미 존재하면 false를 반환합니다.
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

// RemoveRoom: ACL 목록에서 특정 채팅방을 제거합니다.
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

// IsRoomAllowed: 해당 채팅방(chatID)이 봇 사용이 허용된 곳인지 확인합니다.
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

	switch strings.ToLower(strings.TrimSpace(c.ACLMode)) {
	case "blacklist":
		// 블랙리스트: 목록에 있으면 차단
		return !inList
	default:
		// 화이트리스트: 목록에 있으면 허용
		return inList
	}
}
