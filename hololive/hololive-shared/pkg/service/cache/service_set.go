package cache

import (
	"context"
	"log/slog"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/privacylog"
)

func (c *Service) SAdd(ctx context.Context, key string, members []string) (int64, error) {
	if len(members) == 0 {
		return 0, nil
	}

	return c.setMembersMutation(ctx, "sadd", key, c.client.B().Sadd().Key(key).Member(members...).Build())
}

func (c *Service) SRem(ctx context.Context, key string, members []string) (int64, error) {
	if len(members) == 0 {
		return 0, nil
	}

	return c.setMembersMutation(ctx, "srem", key, c.client.B().Srem().Key(key).Member(members...).Build())
}

func (c *Service) setMembersMutation(ctx context.Context, op, key string, cmd valkey.Completed) (int64, error) {
	resp := c.client.Do(ctx, cmd)
	if resp.Error() != nil {
		c.logger.Error("Cache "+op+" failed", privacylog.CacheKeyAttr(key), slog.Any("error", resp.Error()))

		return 0, NewCacheError(op, key, resp.Error())
	}

	changed, err := resp.AsInt64()
	if err != nil {
		return 0, NewCacheError(op, key, err)
	}

	return changed, nil
}

func (c *Service) SMembers(ctx context.Context, key string) ([]string, error) {
	resp := c.client.Do(ctx, c.client.B().Smembers().Key(key).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache smembers failed", privacylog.CacheKeyAttr(key), slog.Any("error", resp.Error()))

		return []string{}, NewCacheError("smembers", key, resp.Error())
	}

	members, err := resp.AsStrSlice()
	if err != nil {
		return []string{}, NewCacheError("smembers", key, err)
	}

	return members, nil
}
