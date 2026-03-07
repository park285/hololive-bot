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

package subscription

import (
	"context"
	"testing"
)

type mockRepository struct{}

func (m *mockRepository) Subscribe(_ context.Context, _, _ string) error { return nil }
func (m *mockRepository) IsSubscribed(_ context.Context, _ string) (bool, error) {
	return true, nil
}
func (m *mockRepository) ListSubscribedRooms(_ context.Context) ([]string, error) {
	return []string{"room-1"}, nil
}

var _ SubscriptionRepository[string] = (*mockRepository)(nil)

func TestSubscriptionRepositoryContract(t *testing.T) {
	t.Parallel()

	repo := &mockRepository{}
	rooms, err := repo.ListSubscribedRooms(context.Background())
	if err != nil {
		t.Fatalf("ListSubscribedRooms() error = %v", err)
	}
	if len(rooms) != 1 || rooms[0] != "room-1" {
		t.Fatalf("ListSubscribedRooms() = %v, want [room-1]", rooms)
	}
}
