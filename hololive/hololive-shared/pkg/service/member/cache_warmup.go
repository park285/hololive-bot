package member

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

func (c *Cache) WarmUpCache(ctx context.Context) error {
	if c == nil {
		return errors.New("member cache is nil")
	}

	_, generation := c.allMembersView()

	snap, err := c.loadAllMembersSnapshot(ctx, generation)
	if err != nil {
		return fmt.Errorf("load all members: %w", err)
	}

	if c.logger != nil {
		c.logger.Info("Member cache warmed up", slog.Int("total_members", len(snap.members)))
	}

	return nil
}
