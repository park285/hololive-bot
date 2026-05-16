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
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

func TestBuildActiveMemberIndex(t *testing.T) {
	members := []*domain.Member{
		{ChannelID: "UC1", Name: "A"},
		{ChannelID: "", Name: "skip-empty"},
		{ChannelID: "UC2", Name: "B", IsGraduated: true},
		{ChannelID: "UC3", Name: "C"},
	}

	ids, names := sharedserver.BuildActiveMemberIndex(members)
	if len(ids) != 2 {
		t.Fatalf("len(ids)=%d want=2 ids=%v", len(ids), ids)
	}

	if ids[0] != "UC1" || ids[1] != "UC3" {
		t.Fatalf("ids=%v want=[UC1 UC3]", ids)
	}

	if names["UC1"] != "A" || names["UC3"] != "C" {
		t.Fatalf("names=%v", names)
	}
}

func TestGetActiveMemberIndex_ConcurrentBuildIsCoalesced(t *testing.T) {
	var callCount atomic.Int32

	handler := &StreamAPIHandler{APIHandler: &APIHandler{
		streamState: newStreamState(),
		memberIndexLoader: func(context.Context) ([]*domain.Member, error) {
			callCount.Add(1)
			time.Sleep(40 * time.Millisecond)

			return []*domain.Member{
				{ChannelID: "UC1", Name: "A"},
				{ChannelID: "UC2", Name: "B"},
				{ChannelID: "", Name: "skip-empty"},
				{ChannelID: "UCX", Name: "skip-graduated", IsGraduated: true},
			}, nil
		},
	}}

	const workers = 20

	var wg sync.WaitGroup

	errCh := make(chan error, workers)

	for range workers {
		wg.Go(func() {
			ids, names, err := handler.getActiveMemberIndex(t.Context())
			if err != nil {
				errCh <- fmt.Errorf("get active member index: %w", err)
				return
			}

			if len(ids) != 2 {
				errCh <- fmt.Errorf("len(ids)=%d want=2", len(ids))
				return
			}

			if names["UC1"] != "A" || names["UC2"] != "B" {
				errCh <- fmt.Errorf("unexpected names map: %v", names)
				return
			}
		})
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatal(err)
	}

	if got := callCount.Load(); got != 1 {
		t.Fatalf("callCount=%d want=1", got)
	}

	_, _, err := handler.getActiveMemberIndex(t.Context())
	if err != nil {
		t.Fatalf("second cache call: %v", err)
	}

	if got := callCount.Load(); got != 1 {
		t.Fatalf("callCount after cache hit=%d want=1", got)
	}
}

func TestMemberToChannelResponse(t *testing.T) {
	tests := []struct {
		name     string
		member   *domain.Member
		expected *sharedserver.ChannelResponse
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
			expected: &sharedserver.ChannelResponse{
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
			expected: &sharedserver.ChannelResponse{
				ID:    "UC456",
				Name:  "No Photo Member",
				Photo: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sharedserver.MemberToChannelResponse(tt.member)
			if tt.expected == nil {
				if got != nil {
					t.Errorf("memberToChannelResponse() = %+v, want nil", got)
				}

				return
			}

			if got == nil {
				t.Errorf("memberToChannelResponse() = nil, want %+v", tt.expected)
				return
			}

			if got.ID != tt.expected.ID {
				t.Errorf("ID = %q, want %q", got.ID, tt.expected.ID)
			}

			if got.Name != tt.expected.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.expected.Name)
			}

			if tt.expected.Photo == nil {
				if got.Photo != nil {
					t.Errorf("Photo = %v, want nil", *got.Photo)
				}
			} else {
				if got.Photo == nil {
					t.Errorf("Photo = nil, want %q", *tt.expected.Photo)
				} else if *got.Photo != *tt.expected.Photo {
					t.Errorf("Photo = %q, want %q", *got.Photo, *tt.expected.Photo)
				}
			}
		})
	}
}
