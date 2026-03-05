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
