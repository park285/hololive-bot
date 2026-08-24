package cache

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/privacylog"
)

func (c *Service) SAdd(ctx context.Context, key string, members []string) (int64, error) {
	if len(members) == 0 {
		return 0, nil
	}

	resp := c.client.Do(ctx, c.client.B().Sadd().Key(key).Member(members...).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache sadd failed", privacylog.CacheKeyAttr(key), slog.Any("error", resp.Error()))

		return 0, NewCacheError("sadd", key, resp.Error())
	}

	added, err := resp.AsInt64()
	if err != nil {
		return 0, NewCacheError("sadd", key, err)
	}

	return added, nil
}

func (c *Service) SRem(ctx context.Context, key string, members []string) (int64, error) {
	if len(members) == 0 {
		return 0, nil
	}

	resp := c.client.Do(ctx, c.client.B().Srem().Key(key).Member(members...).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache srem failed", privacylog.CacheKeyAttr(key), slog.Any("error", resp.Error()))

		return 0, NewCacheError("srem", key, resp.Error())
	}

	removed, err := resp.AsInt64()
	if err != nil {
		return 0, NewCacheError("srem", key, err)
	}

	return removed, nil
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

func (c *Service) SIsMember(ctx context.Context, key, member string) (bool, error) {
	resp := c.client.Do(ctx, c.client.B().Sismember().Key(key).Member(member).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache sismember failed", privacylog.CacheKeyAttr(key), slog.Any("error", resp.Error()))

		return false, NewCacheError("sismember", key, resp.Error())
	}

	exists, err := resp.AsBool()
	if err != nil {
		return false, NewCacheError("sismember", key, err)
	}

	return exists, nil
}
