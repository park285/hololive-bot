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

// setMembersMutation은 SADD·SREM처럼 바뀐 원소 수를 돌려주는 집합 명령을 실행한다.
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
