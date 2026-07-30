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

package cache

import (
	"context"
	"testing"
)

func TestInitializeMemberDatabaseCanonicalFields(t *testing.T) {
	t.Parallel()
	service, _ := newTestCacheService(t)
	ctx := context.Background()
	members := map[string]string{"Aqua:Hololive": "UCOyYb1c43VlX9rc_lT6NKQw"}
	if err := service.InitializeMemberDatabase(ctx, members); err != nil {
		t.Fatalf("InitializeMemberDatabase() error = %v", err)
	}
	got, err := service.GetAllMembers(ctx)
	if err != nil {
		t.Fatalf("GetAllMembers() error = %v", err)
	}
	if got["Aqua:Hololive"] != members["Aqua:Hololive"] {
		t.Fatalf("GetAllMembers() = %#v", got)
	}
}

func TestInitializeMemberDatabaseRejectsOldFieldShape(t *testing.T) {
	t.Parallel()
	service, _ := newTestCacheService(t)

	err := service.InitializeMemberDatabase(t.Context(), map[string]string{"Miko": "channel"})
	if err == nil {
		t.Fatal("InitializeMemberDatabase() error = nil")
	}
	if got, want := err.Error(), "initialize member database: member field must use name:org format"; got != want {
		t.Fatalf("InitializeMemberDatabase() error = %q, want %q", got, want)
	}
}

func TestGetAllMembersIgnoresOldOnlyField(t *testing.T) {
	t.Parallel()
	service, mini := newTestCacheService(t)
	mini.HSet(memberHashKey, "Miko", "old-channel")

	got, err := service.GetAllMembers(t.Context())
	if err != nil {
		t.Fatalf("GetAllMembers() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("GetAllMembers() = %#v, want empty", got)
	}
}

func TestGetMemberChannelIDWithOrgDoesNotReadBareField(t *testing.T) {
	t.Parallel()
	service, mini := newTestCacheService(t)
	mini.HSet(memberHashKey, "Miko", "old-channel")

	got, err := service.GetMemberChannelIDWithOrg(t.Context(), "Miko", "")
	if err != nil {
		t.Fatalf("GetMemberChannelIDWithOrg() error = %v", err)
	}
	if got != "" {
		t.Fatalf("GetMemberChannelIDWithOrg() = %q, want empty", got)
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
			service, _ := newTestCacheService(t)
			ctx := context.Background()

			if err := service.InitializeMemberDatabase(ctx, tt.memberData); err != nil {
				t.Fatalf("InitializeMemberDatabase() error = %v", err)
			}

			gotID, err := service.GetMemberChannelIDWithOrg(ctx, tt.lookupName, tt.lookupOrg)
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
			service, _ := newTestCacheService(t)
			ctx := context.Background()

			if err := service.InitializeMemberDatabase(ctx, tt.memberData); err != nil {
				t.Fatalf("InitializeMemberDatabase() error = %v", err)
			}

			gotIDs, err := service.GetMemberChannelIDs(ctx, tt.lookupName)
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
