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

package acl

import (
	"log/slog"
	"slices"
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
	service := newTestService(false, ACLModeWhitelist, []string{"room-A"}, nil)

	tests := []struct {
		name   string
		chatID string
		want   bool
	}{
		{
			name:   "목록에 없는 방도 허용",
			chatID: "room-unknown",
			want:   true,
		},
		{
			name:   "빈 chatID도 허용",
			chatID: "",
			want:   true,
		},
		{
			name:   "목록에 있는 방도 허용",
			chatID: "room-A",
			want:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := service.IsRoomAllowed(tc.chatID)
			if got != tc.want {
				t.Errorf("IsRoomAllowed(%q) = %v, want %v", tc.chatID, got, tc.want)
			}
		})
	}
}

func TestIsRoomAllowed_WhitelistMode(t *testing.T) {
	t.Parallel()

	service := newTestService(true, ACLModeWhitelist, []string{"chat-alpha", "chat-beta"}, nil)

	tests := []struct {
		name   string
		chatID string
		want   bool
	}{
		{
			name:   "chatID로 화이트리스트 매칭 성공",
			chatID: "chat-beta",
			want:   true,
		},
		{
			name:   "앞뒤 공백은 TrimSpace 후 매칭",
			chatID: "  chat-alpha  ",
			want:   true,
		},
		{
			name:   "화이트리스트에 없는 chatID 거부",
			chatID: "chat-y",
			want:   false,
		},
		{
			name:   "빈 chatID는 거부",
			chatID: "",
			want:   false,
		},
		{
			name:   "공백만 있는 chatID는 TrimSpace 후 빈 문자열이므로 거부",
			chatID: "   ",
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := service.IsRoomAllowed(tc.chatID)
			if got != tc.want {
				t.Errorf("IsRoomAllowed(%q) = %v, want %v", tc.chatID, got, tc.want)
			}
		})
	}
}

func TestIsRoomAllowed_BlacklistMode(t *testing.T) {
	t.Parallel()

	service := newTestService(true, ACLModeBlacklist, nil, []string{testBlockedRoom, "blocked-chat"})

	tests := []struct {
		name   string
		chatID string
		want   bool
	}{
		{
			name:   "블랙리스트에 없는 chatID는 허용",
			chatID: "allowed-chat",
			want:   true,
		},
		{
			name:   "블랙리스트에 있는 chatID는 차단",
			chatID: "blocked-chat",
			want:   false,
		},
		{
			name:   "빈 chatID이면 블랙리스트 매칭 없으므로 허용",
			chatID: "",
			want:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := service.IsRoomAllowed(tc.chatID)
			if got != tc.want {
				t.Errorf("IsRoomAllowed(%q) = %v, want %v", tc.chatID, got, tc.want)
			}
		})
	}
}

// 방 이름으로 등록된 과거 값은 다른 chatID의 방을 허용하거나 차단하지 않는다
// (DEC-20260926-stack-hololive-room-acl-and-console-contract).
func TestIsRoomAllowed_RoomNameEntryDoesNotMatchChatID(t *testing.T) {
	t.Parallel()

	whitelist := newTestService(true, ACLModeWhitelist, []string{"테스트방"}, nil)
	if whitelist.IsRoomAllowed("1234567890") {
		t.Error("화이트리스트의 방 이름 항목이 다른 chatID를 허용하면 안 됨")
	}

	blacklist := newTestService(true, ACLModeBlacklist, nil, []string{"차단방"})
	if !blacklist.IsRoomAllowed("1234567890") {
		t.Error("블랙리스트의 방 이름 항목이 다른 chatID를 차단하면 안 됨")
	}
}

func TestIsRoomAllowed_EmptyWhitelist(t *testing.T) {
	t.Parallel()

	// ACL 활성화 + 화이트리스트 비어있음 → 모든 방 거부
	service := newTestService(true, ACLModeWhitelist, []string{}, nil)

	got := service.IsRoomAllowed("any-chat")
	if got {
		t.Error("화이트리스트가 비어있을 때 IsRoomAllowed는 false여야 함")
	}
}

func TestIsRoomAllowed_EmptyBlacklist(t *testing.T) {
	t.Parallel()

	// ACL 활성화 + 블랙리스트 비어있음 → 모든 방 허용
	service := newTestService(true, ACLModeBlacklist, nil, []string{})

	got := service.IsRoomAllowed("any-chat")
	if !got {
		t.Error("블랙리스트가 비어있을 때 IsRoomAllowed는 true여야 함")
	}
}

func TestIsRoomAllowed_DualLists_Independent(t *testing.T) {
	t.Parallel()

	// 화이트리스트와 블랙리스트에 각각 다른 방이 있을 때 현재 모드만 참조하는지 검증
	service := newTestService(true, ACLModeWhitelist, []string{"allowed-only"}, []string{"blocked-only"})

	// 화이트리스트 모드: allowed-only만 허용, blocked-only는 화이트리스트에 없으므로 거부
	if !service.IsRoomAllowed("allowed-only") {
		t.Error("화이트리스트 모드에서 화이트리스트에 있는 방은 허용되어야 함")
	}

	if service.IsRoomAllowed("blocked-only") {
		t.Error("화이트리스트 모드에서 화이트리스트에 없는 방은 거부되어야 함")
	}

	// 블랙리스트 모드로 전환 (메모리만 변경, DB/캐시 없음)
	service.mu.Lock()

	service.mode = ACLModeBlacklist
	service.mu.Unlock()

	// 블랙리스트 모드: blocked-only는 차단, allowed-only는 블랙리스트에 없으므로 허용
	if service.IsRoomAllowed("blocked-only") {
		t.Error("블랙리스트 모드에서 블랙리스트에 있는 방은 차단되어야 함")
	}

	if !service.IsRoomAllowed("allowed-only") {
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
			blRooms:      []string{testBlockedRoom},
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

			service := newTestService(tc.enabled, tc.mode, tc.wlRooms, tc.blRooms)
			assertACLStatus(t, service, tc.wantEnabled, tc.wantMode, tc.wantRoomsCnt, expectedACLRooms(tc.mode, tc.wlRooms, tc.blRooms))
		})
	}
}

func assertACLStatus(t *testing.T, service *Service, wantEnabled bool, wantMode ACLMode, wantRoomsCnt int, expectedRooms []string) {
	t.Helper()

	gotEnabled, gotMode, gotRooms := service.GetACLStatus()
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

	slices.Sort(gotRooms)
	slices.Sort(wantRooms)

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
	service := newTestService(true, ACLModeWhitelist, []string{"room-safe"}, nil)

	_, _, rooms := service.GetACLStatus()
	origLen := len(rooms)

	// 반환된 슬라이스를 수정해도 서비스 내부 상태에 영향 없어야 한다
	_ = append(rooms, "injected-room")

	_, _, roomsAfter := service.GetACLStatus()
	if len(roomsAfter) != origLen {
		t.Errorf("GetACLStatus 반환 슬라이스 수정이 내부 상태에 영향을 줌: got %d rooms, want %d", len(roomsAfter), origLen)
	}
}

func TestGetACLStatus_ReturnsSortedRooms(t *testing.T) {
	t.Parallel()

	service := newTestService(true, ACLModeWhitelist, []string{testRoomB, testRoomA, "room-c"}, nil)

	_, _, rooms := service.GetACLStatus()
	if len(rooms) != 3 {
		t.Fatalf("expected 3 rooms, got %d", len(rooms))
	}

	want := []string{testRoomA, testRoomB, "room-c"}
	for i := range want {
		if rooms[i] != want[i] {
			t.Fatalf("rooms[%d] = %q, want %q (full=%v)", i, rooms[i], want[i], rooms)
		}
	}
}

func TestIsRoomAllowed_ConcurrentRead(t *testing.T) {
	t.Parallel()

	// 동시 읽기 시 race condition 없어야 한다
	service := newTestService(true, ACLModeWhitelist, []string{"room-concurrent"}, nil)

	const goroutines = 50

	var wg sync.WaitGroup

	for range goroutines {
		wg.Go(func() {
			_ = service.IsRoomAllowed("room-concurrent")
		})
	}

	wg.Wait()
}

func TestGetACLStatus_ConcurrentRead(t *testing.T) {
	t.Parallel()

	// 동시 읽기 시 race condition 없어야 한다
	service := newTestService(true, ACLModeBlacklist, nil, []string{testRoomA, testRoomB})

	const goroutines = 50

	var wg sync.WaitGroup

	for range goroutines {
		wg.Go(func() {
			_, _, _ = service.GetACLStatus()
		})
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

func TestParseACLModeStrictRejectsUnknownValue(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "unknown"} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			if _, err := ParseACLModeStrict(input); err == nil {
				t.Fatalf("ParseACLModeStrict(%q) error = nil, want invalid mode error", input)
			}
		})
	}
}
