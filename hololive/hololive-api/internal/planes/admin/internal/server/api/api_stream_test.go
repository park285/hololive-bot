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

package api

import (
	"testing"

	adminhandlers "github.com/kapu/hololive-api/internal/planes/admin/internal/httpapi/handlers"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestMemberToChannelResponse(t *testing.T) {
	tests := []struct {
		name     string
		member   *domain.Member
		expected *adminhandlers.ChannelResponse
	}{
		{
			name:     "nil member",
			member:   nil,
			expected: nil,
		},
		{
			name: "member with photo",
			member: &domain.Member{
				ChannelID: "UC123",
				Name:      "Test Member",
				Photo:     "https://example.com/photo.jpg",
			},
			expected: &adminhandlers.ChannelResponse{
				ID:    "UC123",
				Name:  "Test Member",
				Photo: new("https://example.com/photo.jpg"),
			},
		},
		{
			name: "member without photo",
			member: &domain.Member{
				ChannelID: "UC456",
				Name:      "No Photo Member",
				Photo:     "",
			},
			expected: &adminhandlers.ChannelResponse{
				ID:    "UC456",
				Name:  "No Photo Member",
				Photo: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adminhandlers.MemberToChannelResponse(tt.member)
			assertChannelResponse(t, got, tt.expected)
		})
	}
}

func assertChannelResponse(t *testing.T, got, expected *adminhandlers.ChannelResponse) {
	t.Helper()

	if expected == nil {
		if got != nil {
			t.Errorf("memberToChannelResponse() = %+v, want nil", got)
		}

		return
	}

	if got == nil {
		t.Errorf("memberToChannelResponse() = nil, want %+v", expected)

		return
	}

	if got.ID != expected.ID {
		t.Errorf("ID = %q, want %q", got.ID, expected.ID)
	}

	if got.Name != expected.Name {
		t.Errorf("Name = %q, want %q", got.Name, expected.Name)
	}

	assertChannelResponsePhoto(t, got.Photo, expected.Photo)
}

func assertChannelResponsePhoto(t *testing.T, got, expected *string) {
	t.Helper()

	if expected == nil {
		if got != nil {
			t.Errorf("Photo = %v, want nil", *got)
		}

		return
	}

	if got == nil {
		t.Errorf("Photo = nil, want %q", *expected)

		return
	}

	if *got != *expected {
		t.Errorf("Photo = %q, want %q", *got, *expected)
	}
}
