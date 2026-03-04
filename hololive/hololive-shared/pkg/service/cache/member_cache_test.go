package cache

import (
	"context"
	"testing"
)

func TestInitializeMemberDatabaseAndGetMemberChannelID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		memberData map[string]string
		lookupKey  string
		wantID     string
	}{
		{
			name: "org 키 형식(name:Hololive)으로 저장 후 조회 - 성공",
			memberData: map[string]string{
				"Aqua:Hololive": "UCOyYb1c43VlX9rc_lT6NKQw",
			},
			lookupKey: "Aqua",
			wantID:    "UCOyYb1c43VlX9rc_lT6NKQw",
		},
		{
			name: "레거시 키 형식(name만)으로 저장 후 조회 - 성공",
			memberData: map[string]string{
				"Shion": "UCXTpFs_3PqI41qX2d9tL2Rg",
			},
			lookupKey: "Shion",
			wantID:    "UCXTpFs_3PqI41qX2d9tL2Rg",
		},
		{
			name: "존재하지 않는 멤버 조회 - 빈 문자열 반환",
			memberData: map[string]string{
				"Aqua:Hololive": "UCOyYb1c43VlX9rc_lT6NKQw",
			},
			lookupKey: "Pekora",
			wantID:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, _ := newTestCacheService(t)
			ctx := context.Background()

			if err := svc.InitializeMemberDatabase(ctx, tt.memberData); err != nil {
				t.Fatalf("InitializeMemberDatabase() error = %v", err)
			}

			gotID, err := svc.GetMemberChannelID(ctx, tt.lookupKey)
			if err != nil {
				t.Fatalf("GetMemberChannelID() error = %v", err)
			}
			if gotID != tt.wantID {
				t.Errorf("GetMemberChannelID(%q) = %q, want %q", tt.lookupKey, gotID, tt.wantID)
			}
		})
	}
}

func TestGetMemberChannelID_EmptyName(t *testing.T) {
	t.Parallel()

	svc, _ := newTestCacheService(t)
	ctx := context.Background()

	gotID, err := svc.GetMemberChannelID(ctx, "")
	if err != nil {
		t.Fatalf("GetMemberChannelID(\"\") error = %v", err)
	}
	if gotID != "" {
		t.Errorf("GetMemberChannelID(\"\") = %q, want %q", gotID, "")
	}
}

func TestGetMemberChannelIDWithOrg(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		memberData map[string]string
		lookupName string
		lookupOrg  string
		wantID     string
	}{
		{
			name: "올바른 org로 멤버 조회 - 성공",
			memberData: map[string]string{
				"Noel:Hololive": "UCdyqAaZDKHXg4Ahi7VENnSA",
			},
			lookupName: "Noel",
			lookupOrg:  "Hololive",
			wantID:     "UCdyqAaZDKHXg4Ahi7VENnSA",
		},
		{
			name: "다른 org로 조회 - 빈 문자열 반환",
			memberData: map[string]string{
				"Noel:Hololive": "UCdyqAaZDKHXg4Ahi7VENnSA",
			},
			lookupName: "Noel",
			lookupOrg:  "VSpo",
			wantID:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, _ := newTestCacheService(t)
			ctx := context.Background()

			if err := svc.InitializeMemberDatabase(ctx, tt.memberData); err != nil {
				t.Fatalf("InitializeMemberDatabase() error = %v", err)
			}

			gotID, err := svc.GetMemberChannelIDWithOrg(ctx, tt.lookupName, tt.lookupOrg)
			if err != nil {
				t.Fatalf("GetMemberChannelIDWithOrg() error = %v", err)
			}
			if gotID != tt.wantID {
				t.Errorf("GetMemberChannelIDWithOrg(%q, %q) = %q, want %q",
					tt.lookupName, tt.lookupOrg, gotID, tt.wantID)
			}
		})
	}
}

func TestGetMemberChannelIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		memberData map[string]string
		lookupName string
		wantCount  int
	}{
		{
			name: "동일 이름의 멤버가 여러 org에 있을 때 - 모두 반환",
			memberData: map[string]string{
				"Fubuki:Hololive": "UCdn5BQ06XqgXoAxIhbqw5Rg",
				"Fubuki:VSpo":     "UCFakeVspoFubuki123456789",
			},
			lookupName: "Fubuki",
			wantCount:  2,
		},
		{
			name: "단일 org의 멤버 - 1개 반환",
			memberData: map[string]string{
				"Korone:Hololive": "UChAnqc_AY5_I3Px5dig3X1Q",
			},
			lookupName: "Korone",
			wantCount:  1,
		},
		{
			name: "존재하지 않는 멤버 - 빈 슬라이스 반환",
			memberData: map[string]string{
				"Miko:Hololive": "UC-hM6YJuNYVAmUWxeIr9FeA",
			},
			lookupName: "Pekora",
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, _ := newTestCacheService(t)
			ctx := context.Background()

			if err := svc.InitializeMemberDatabase(ctx, tt.memberData); err != nil {
				t.Fatalf("InitializeMemberDatabase() error = %v", err)
			}

			gotIDs, err := svc.GetMemberChannelIDs(ctx, tt.lookupName)
			if err != nil {
				t.Fatalf("GetMemberChannelIDs() error = %v", err)
			}
			if len(gotIDs) != tt.wantCount {
				t.Errorf("GetMemberChannelIDs(%q) count = %d, want %d",
					tt.lookupName, len(gotIDs), tt.wantCount)
			}
		})
	}
}
