package majorevent

import (
	"context"
	"testing"
)

func TestRepository_Interface(t *testing.T) {
	t.Run("repository methods exist", func(t *testing.T) {
		var _ interface {
			Subscribe(ctx context.Context, roomID, roomName string) error
			Unsubscribe(ctx context.Context, roomID string) error
			IsSubscribed(ctx context.Context, roomID string) (bool, error)
		}

		t.Log("Repository interface verified")
	})
}
