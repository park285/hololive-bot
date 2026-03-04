// Package acl: ACL 서비스의 in-memory 로직 단위 테스트.
// DB/캐시를 요구하는 SetEnabled, AddRoom, RemoveRoom은 통합 테스트 범주이므로
// 여기서는 순수 메모리 상태(IsRoomAllowed, GetACLStatus)만 테스트한다.
package acl

import (
	"io"
	"log/slog"
	"sort"
	"sync"
	"testing"
)

// newTestService: 테스트용 Service 직접 초기화 (DB/캐시 없음)
func newTestService(enabled bool, rooms []string) *Service {
	r := make(map[string]struct{}, len(rooms))
	for _, room := range rooms {
		r[room] = struct{}{}
	}
	return &Service{
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		enabled: enabled,
		rooms:   r,
	}
}

func TestIsRoomAllowed_ACLDisabled(t *testing.T) {
	t.Parallel()

	// ACL 비활성화 시 모든 방이 허용되어야 한다
	svc := newTestService(false, []string{"room-A"})

	tests := []struct {
		name     string
		roomName string
		chatID   string
		want     bool
	}{
		{
			name:     "화이트리스트에 없는 방도 허용",
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
			name:     "화이트리스트에 있는 방도 허용",
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

func TestIsRoomAllowed_ACLEnabled(t *testing.T) {
	t.Parallel()

	svc := newTestService(true, []string{"room-alpha", "chat-beta"})

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

func TestIsRoomAllowed_EmptyWhitelist(t *testing.T) {
	t.Parallel()

	// ACL 활성화 + 화이트리스트 비어있음 → 모든 방 거부
	svc := newTestService(true, []string{})

	got := svc.IsRoomAllowed("any-room", "any-chat")
	if got {
		t.Error("화이트리스트가 비어있을 때 IsRoomAllowed는 false여야 함")
	}
}

func TestGetACLStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		enabled      bool
		rooms        []string
		wantEnabled  bool
		wantRoomsCnt int
	}{
		{
			name:         "활성화 + 방 2개",
			enabled:      true,
			rooms:        []string{"room-1", "room-2"},
			wantEnabled:  true,
			wantRoomsCnt: 2,
		},
		{
			name:         "비활성화 + 방 없음",
			enabled:      false,
			rooms:        []string{},
			wantEnabled:  false,
			wantRoomsCnt: 0,
		},
		{
			name:         "비활성화 + 방 1개",
			enabled:      false,
			rooms:        []string{"room-only"},
			wantEnabled:  false,
			wantRoomsCnt: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			svc := newTestService(tc.enabled, tc.rooms)
			gotEnabled, gotRooms := svc.GetACLStatus()

			if gotEnabled != tc.wantEnabled {
				t.Errorf("GetACLStatus().enabled = %v, want %v", gotEnabled, tc.wantEnabled)
			}
			if len(gotRooms) != tc.wantRoomsCnt {
				t.Errorf("GetACLStatus().rooms len = %d, want %d", len(gotRooms), tc.wantRoomsCnt)
			}

			// 반환된 방 목록이 초기화 시 지정한 방들을 모두 포함하는지 검증
			sort.Strings(gotRooms)
			inputCopy := make([]string, len(tc.rooms))
			copy(inputCopy, tc.rooms)
			sort.Strings(inputCopy)
			for i, r := range inputCopy {
				if gotRooms[i] != r {
					t.Errorf("rooms[%d] = %q, want %q", i, gotRooms[i], r)
				}
			}
		})
	}
}

func TestGetACLStatus_ReturnsCopy(t *testing.T) {
	t.Parallel()

	// GetACLStatus가 내부 맵의 복사본을 반환해야 한다 (외부 수정이 내부 상태에 영향 없어야 함)
	svc := newTestService(true, []string{"room-safe"})

	_, rooms := svc.GetACLStatus()
	origLen := len(rooms)

	// 반환된 슬라이스를 수정해도 서비스 내부 상태에 영향 없어야 한다
	rooms = append(rooms, "injected-room")

	_, roomsAfter := svc.GetACLStatus()
	if len(roomsAfter) != origLen {
		t.Errorf("GetACLStatus 반환 슬라이스 수정이 내부 상태에 영향을 줌: got %d rooms, want %d", len(roomsAfter), origLen)
	}
}

func TestIsRoomAllowed_ConcurrentRead(t *testing.T) {
	t.Parallel()

	// 동시 읽기 시 race condition 없어야 한다
	svc := newTestService(true, []string{"room-concurrent"})

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
	svc := newTestService(true, []string{"room-a", "room-b"})

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			_, _ = svc.GetACLStatus()
		}()
	}

	wg.Wait()
}
