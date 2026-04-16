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

// Package acl: ACL 서비스의 in-memory 로직 단위 테스트.
// DB/캐시를 요구하는 SetEnabled, SetMode, AddRoom, RemoveRoom은 통합 테스트 범주이므로
// 여기서는 순수 메모리 상태(IsRoomAllowed, GetACLStatus)만 테스트한다.
package acl

import (
	"log/slog"
	"sort"
	"sync"
	"testing"
)

// newTestService: 테스트용 Service 직접 초기화 (DB/캐시 없음).
func newTestService(enabled bool, mode ACLMode, whitelistRooms, blacklistRooms []string) *Service {
	wl := make(map[string]struct{}, len(whitelistRooms))
	for _, room := range whitelistRooms {
		wl[room] = struct{}{}
	}

	bl := make(map[string]struct{}, len(blacklistRooms))
	for _, room := range blacklistRooms {
		bl[room] = struct{}{}
	}

	return &Service{
		logger:         slog.New(slog.DiscardHandler),
		enabled:        enabled,
		mode:           mode,
		whitelistRooms: wl,
		blacklistRooms: bl,
	}
}

func TestIsRoomAllowed_ACLDisabled(t *testing.T) {
	t.Parallel()

	// ACL 비활성화 시 모든 방이 허용되어야 한다
	svc := newTestService(false, ACLModeWhitelist, []string{"room-A"}, nil)

	tests := []struct {
		name     string
		roomName string
		chatID   string
		want     bool
	}{
		{
			name:     "목록에 없는 방도 허용",
			roomName: "room-unknown",
			chatID:   "",
			want:     true,
		},
		{
			name:     "빈 roomName/chatID도 허용",
			roomName: "",
			chatID:   "",
			want:     true,
		},
		{
			name:     "목록에 있는 방도 허용",
			roomName: "room-A",
			chatID:   "",
			want:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := svc.IsRoomAllowed(tc.roomName, tc.chatID)
			if got != tc.want {
				t.Errorf("IsRoomAllowed(%q, %q) = %v, want %v", tc.roomName, tc.chatID, got, tc.want)
			}
		})
	}
}

func TestIsRoomAllowed_WhitelistMode(t *testing.T) {
	t.Parallel()

	svc := newTestService(true, ACLModeWhitelist, []string{"room-alpha", "chat-beta"}, nil)

	tests := []struct {
		name     string
		roomName string
		chatID   string
		want     bool
	}{
		{
			name:     "roomName으로 화이트리스트 매칭 성공",
			roomName: "room-alpha",
			chatID:   "",
			want:     true,
		},
		{
			name:     "chatID로 화이트리스트 매칭 성공",
			roomName: "",
			chatID:   "chat-beta",
			want:     true,
		},
		{
			name:     "chatID 우선 매칭 (chatID가 있으면 roomName보다 먼저 확인)",
			roomName: "not-in-list",
			chatID:   "chat-beta",
			want:     true,
		},
		{
			name:     "화이트리스트에 없는 방 거부",
			roomName: "room-unknown",
			chatID:   "",
			want:     false,
		},
		{
			name:     "양쪽 모두 화이트리스트에 없으면 거부",
			roomName: "room-x",
			chatID:   "chat-y",
			want:     false,
		},
		{
			name:     "빈 roomName/chatID는 거부",
			roomName: "",
			chatID:   "",
			want:     false,
		},
		{
			name:     "공백만 있는 roomName은 TrimSpace 후 빈 문자열이므로 거부",
			roomName: "   ",
			chatID:   "   ",
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := svc.IsRoomAllowed(tc.roomName, tc.chatID)
			if got != tc.want {
				t.Errorf("IsRoomAllowed(%q, %q) = %v, want %v", tc.roomName, tc.chatID, got, tc.want)
			}
		})
	}
}

func TestIsRoomAllowed_BlacklistMode(t *testing.T) {
	t.Parallel()

	svc := newTestService(true, ACLModeBlacklist, nil, []string{"blocked-room", "blocked-chat"})

	tests := []struct {
		name     string
		roomName string
		chatID   string
		want     bool
	}{
		{
			name:     "블랙리스트에 없는 방은 허용",
			roomName: "allowed-room",
			chatID:   "",
			want:     true,
		},
		{
			name:     "블랙리스트에 있는 방은 차단 (roomName)",
			roomName: "blocked-room",
			chatID:   "",
			want:     false,
		},
		{
			name:     "블랙리스트에 있는 방은 차단 (chatID)",
			roomName: "",
			chatID:   "blocked-chat",
			want:     false,
		},
		{
			name:     "chatID가 블랙리스트에 있으면 차단 (roomName은 무관)",
			roomName: "safe-room",
			chatID:   "blocked-chat",
			want:     false,
		},
		{
			name:     "빈 roomName/chatID이면 블랙리스트 매칭 없으므로 허용",
			roomName: "",
			chatID:   "",
			want:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := svc.IsRoomAllowed(tc.roomName, tc.chatID)
			if got != tc.want {
				t.Errorf("IsRoomAllowed(%q, %q) = %v, want %v", tc.roomName, tc.chatID, got, tc.want)
			}
		})
	}
}

func TestIsRoomAllowed_EmptyWhitelist(t *testing.T) {
	t.Parallel()

	// ACL 활성화 + 화이트리스트 비어있음 → 모든 방 거부
	svc := newTestService(true, ACLModeWhitelist, []string{}, nil)

	got := svc.IsRoomAllowed("any-room", "any-chat")
	if got {
		t.Error("화이트리스트가 비어있을 때 IsRoomAllowed는 false여야 함")
	}
}

func TestIsRoomAllowed_EmptyBlacklist(t *testing.T) {
	t.Parallel()

	// ACL 활성화 + 블랙리스트 비어있음 → 모든 방 허용
	svc := newTestService(true, ACLModeBlacklist, nil, []string{})

	got := svc.IsRoomAllowed("any-room", "any-chat")
	if !got {
		t.Error("블랙리스트가 비어있을 때 IsRoomAllowed는 true여야 함")
	}
}

func TestIsRoomAllowed_DualLists_Independent(t *testing.T) {
	t.Parallel()

	// 화이트리스트와 블랙리스트에 각각 다른 방이 있을 때 현재 모드만 참조하는지 검증
	svc := newTestService(true, ACLModeWhitelist, []string{"allowed-only"}, []string{"blocked-only"})

	// 화이트리스트 모드: allowed-only만 허용, blocked-only는 화이트리스트에 없으므로 거부
	if !svc.IsRoomAllowed("allowed-only", "") {
		t.Error("화이트리스트 모드에서 화이트리스트에 있는 방은 허용되어야 함")
	}

	if svc.IsRoomAllowed("blocked-only", "") {
		t.Error("화이트리스트 모드에서 화이트리스트에 없는 방은 거부되어야 함")
	}

	// 블랙리스트 모드로 전환 (메모리만 변경, DB/캐시 없음)
	svc.mu.Lock()
	svc.mode = ACLModeBlacklist
	svc.mu.Unlock()

	// 블랙리스트 모드: blocked-only는 차단, allowed-only는 블랙리스트에 없으므로 허용
	if svc.IsRoomAllowed("blocked-only", "") {
		t.Error("블랙리스트 모드에서 블랙리스트에 있는 방은 차단되어야 함")
	}

	if !svc.IsRoomAllowed("allowed-only", "") {
		t.Error("블랙리스트 모드에서 블랙리스트에 없는 방은 허용되어야 함")
	}
}

func TestGetACLStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		enabled      bool
		mode         ACLMode
		wlRooms      []string
		blRooms      []string
		wantEnabled  bool
		wantMode     ACLMode
		wantRoomsCnt int
	}{
		{
			name:         "화이트리스트 활성화 + 방 2개",
			enabled:      true,
			mode:         ACLModeWhitelist,
			wlRooms:      []string{"room-1", "room-2"},
			wantEnabled:  true,
			wantMode:     ACLModeWhitelist,
			wantRoomsCnt: 2,
		},
		{
			name:         "블랙리스트 활성화 + 방 1개",
			enabled:      true,
			mode:         ACLModeBlacklist,
			blRooms:      []string{"blocked-room"},
			wantEnabled:  true,
			wantMode:     ACLModeBlacklist,
			wantRoomsCnt: 1,
		},
		{
			name:         "비활성화 + 방 없음",
			enabled:      false,
			mode:         ACLModeWhitelist,
			wantEnabled:  false,
			wantMode:     ACLModeWhitelist,
			wantRoomsCnt: 0,
		},
		{
			name:         "비활성화 + 블랙리스트 + 방 1개",
			enabled:      false,
			mode:         ACLModeBlacklist,
			blRooms:      []string{"room-only"},
			wantEnabled:  false,
			wantMode:     ACLModeBlacklist,
			wantRoomsCnt: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := newTestService(tc.enabled, tc.mode, tc.wlRooms, tc.blRooms)
			assertACLStatus(t, svc, tc.wantEnabled, tc.wantMode, tc.wantRoomsCnt, expectedACLRooms(tc.mode, tc.wlRooms, tc.blRooms))
		})
	}
}

func assertACLStatus(t *testing.T, svc *Service, wantEnabled bool, wantMode ACLMode, wantRoomsCnt int, expectedRooms []string) {
	t.Helper()

	gotEnabled, gotMode, gotRooms := svc.GetACLStatus()
	if gotEnabled != wantEnabled {
		t.Errorf("GetACLStatus().enabled = %v, want %v", gotEnabled, wantEnabled)
	}

	if gotMode != wantMode {
		t.Errorf("GetACLStatus().mode = %v, want %v", gotMode, wantMode)
	}

	if len(gotRooms) != wantRoomsCnt {
		t.Errorf("GetACLStatus().rooms len = %d, want %d", len(gotRooms), wantRoomsCnt)
	}

	wantRooms := append([]string(nil), expectedRooms...)

	sort.Strings(gotRooms)
	sort.Strings(wantRooms)

	for i, room := range wantRooms {
		if gotRooms[i] != room {
			t.Errorf("rooms[%d] = %q, want %q", i, gotRooms[i], room)
		}
	}
}

func expectedACLRooms(mode ACLMode, whitelistRooms, blacklistRooms []string) []string {
	if mode == ACLModeBlacklist {
		return blacklistRooms
	}

	return whitelistRooms
}

func TestGetACLStatus_ReturnsCopy(t *testing.T) {
	t.Parallel()

	// GetACLStatus가 내부 맵의 복사본을 반환해야 한다 (외부 수정이 내부 상태에 영향 없어야 함)
	svc := newTestService(true, ACLModeWhitelist, []string{"room-safe"}, nil)

	_, _, rooms := svc.GetACLStatus()
	origLen := len(rooms)

	// 반환된 슬라이스를 수정해도 서비스 내부 상태에 영향 없어야 한다
	_ = append(rooms, "injected-room")

	_, _, roomsAfter := svc.GetACLStatus()
	if len(roomsAfter) != origLen {
		t.Errorf("GetACLStatus 반환 슬라이스 수정이 내부 상태에 영향을 줌: got %d rooms, want %d", len(roomsAfter), origLen)
	}
}

func TestGetACLStatus_ReturnsSortedRooms(t *testing.T) {
	t.Parallel()

	svc := newTestService(true, ACLModeWhitelist, []string{"room-b", "room-a", "room-c"}, nil)

	_, _, rooms := svc.GetACLStatus()
	if len(rooms) != 3 {
		t.Fatalf("expected 3 rooms, got %d", len(rooms))
	}

	want := []string{"room-a", "room-b", "room-c"}
	for i := range want {
		if rooms[i] != want[i] {
			t.Fatalf("rooms[%d] = %q, want %q (full=%v)", i, rooms[i], want[i], rooms)
		}
	}
}

func TestIsRoomAllowed_ConcurrentRead(t *testing.T) {
	t.Parallel()

	// 동시 읽기 시 race condition 없어야 한다
	svc := newTestService(true, ACLModeWhitelist, []string{"room-concurrent"}, nil)

	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()

			_ = svc.IsRoomAllowed("room-concurrent", "")
		}()
	}

	wg.Wait()
}

func TestGetACLStatus_ConcurrentRead(t *testing.T) {
	t.Parallel()

	// 동시 읽기 시 race condition 없어야 한다
	svc := newTestService(true, ACLModeBlacklist, nil, []string{"room-a", "room-b"})

	const goroutines = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()

			_, _, _ = svc.GetACLStatus()
		}()
	}

	wg.Wait()
}

func TestParseACLMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  ACLMode
	}{
		{"whitelist", ACLModeWhitelist},
		{"WHITELIST", ACLModeWhitelist},
		{"blacklist", ACLModeBlacklist},
		{"BLACKLIST", ACLModeBlacklist},
		{"  blacklist  ", ACLModeBlacklist},
		{"", ACLModeWhitelist},
		{"unknown", ACLModeWhitelist},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			got := ParseACLMode(tc.input)
			if got != tc.want {
				t.Errorf("ParseACLMode(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
