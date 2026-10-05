package cache

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/privacylog"
	"github.com/kapu/hololive-shared/pkg/util"
)

func (c *Service) Get(ctx context.Context, key string, dest any) error {
	if _, err := c.GetJSON(ctx, key, dest); err != nil {
		return fmt.Errorf("get json: %w", err)
	}

	return nil
}

func (c *Service) GetJSON(ctx context.Context, key string, dest any) (bool, error) {
	value, hit, err := c.GetString(ctx, key)
	if err != nil {
		return hit, fmt.Errorf("get string: %w", err)
	}

	if !hit {
		return false, nil
	}

	if dest != nil {
		if err := jsonv2.Unmarshal([]byte(value), dest); err != nil {
			c.logger.Error("Cache value unmarshal failed", privacylog.CacheKeyAttr(key), slog.Any("error", err))

			return true, NewCacheError("get", key, err)
		}
	}

	return true, nil
}

func (c *Service) GetString(ctx context.Context, key string) (string, bool, error) {
	resp := c.client.Do(ctx, c.client.B().Get().Key(key).Build())
	if util.IsValkeyNil(resp.Error()) {
		return "", false, nil
	}

	if resp.Error() != nil {
		c.logger.Error("Cache get operation failed", privacylog.CacheKeyAttr(key), slog.Any("error", resp.Error()))

		return "", false, NewCacheError("get", key, resp.Error())
	}

	value, err := resp.ToString()
	if err != nil {
		c.logger.Error("Cache value conversion failed", privacylog.CacheKeyAttr(key), slog.Any("error", err))

		return "", false, NewCacheError("get", key, err)
	}

	return value, true, nil
}

func ttlSecondsCeil(ttl time.Duration) (int64, error) {
	if ttl < 0 {
		return 0, errors.New("ttl must not be negative")
	}

	if ttl == 0 {
		return 0, nil
	}

	seconds := int64(math.Ceil(ttl.Seconds()))
	if seconds <= 0 {
		seconds = 1
	}

	return seconds, nil
}

func (c *Service) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	jsonData, err := jsonv2.Marshal(value)
	if err != nil {
		return NewCacheError("set", key, err)
	}

	var cmd valkey.Completed

	if ttl > 0 {
		ttlSeconds, err := ttlSecondsCeil(ttl)
		if err != nil {
			return NewCacheError("set", key, err)
		}

		cmd = c.client.B().Set().Key(key).Value(string(jsonData)).ExSeconds(ttlSeconds).Build()
	} else {
		cmd = c.client.B().Set().Key(key).Value(string(jsonData)).Build()
	}

	if err := c.client.Do(ctx, cmd).Error(); err != nil {
		c.logger.Error("Cache set failed", privacylog.CacheKeyAttr(key), slog.Any("error", err))

		return NewCacheError("set", key, err)
	}

	return nil
}

func (c *Service) Del(ctx context.Context, key string) error {
	if err := c.client.Do(ctx, c.client.B().Del().Key(key).Build()).Error(); err != nil {
		c.logger.Error("Cache delete failed", privacylog.CacheKeyAttr(key), slog.Any("error", err))

		return NewCacheError("del", key, err)
	}

	return nil
}

func (c *Service) DelMany(ctx context.Context, keys []string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}

	const delManyChunkSize = 500

	var totalDeleted int64

	for start := 0; start < len(keys); start += delManyChunkSize {
		end := min(start+delManyChunkSize, len(keys))

		chunk := keys[start:end]
		resp := c.client.Do(ctx, c.client.B().Del().Key(chunk...).Build())

		if resp.Error() != nil {
			c.logger.Error("Cache delete many failed", slog.Int("count", len(chunk)), slog.Any("error", resp.Error()))

			return totalDeleted, NewCacheError("del", fmt.Sprintf("%d keys", len(chunk)), resp.Error())
		}

		deleted, err := resp.AsInt64()
		if err != nil {
			return totalDeleted, NewCacheError("del", "", err)
		}

		totalDeleted += deleted
	}

	return totalDeleted, nil
}

// ScanKeyPages는 SCAN 응답 page마다 visit를 호출해 전체 key 목록을 메모리에 모으지 않는다.
// KEYS와 달리 Redis를 블로킹하지 않지만 비원자적이므로 scan 중 바뀐 key는 누락·중복될 수 있다.
// Visit가 오류를 반환하면 순회를 멈추고 그 오류를 감싸 반환한다. Visit에 넘긴 slice는 호출 뒤 재사용하지 않는다.
func (c *Service) ScanKeyPages(ctx context.Context, pattern string, batchSize int64, visit func(keys []string) error) error {
	if batchSize <= 0 {
		batchSize = 100
	}

	cursor := uint64(0)

	for {
		cmd := c.client.B().Scan().Cursor(cursor).Match(pattern).Count(batchSize).Build()
		resp := c.client.Do(ctx, cmd)

		if resp.Error() != nil {
			c.logger.Error("Cache scan failed", slog.String("pattern", pattern), slog.Any("error", resp.Error()))

			return NewCacheError("scan", pattern, resp.Error())
		}

		entry, err := resp.AsScanEntry()
		if err != nil {
			return NewCacheError("scan", pattern, err)
		}

		if len(entry.Elements) > 0 {
			if err := visit(entry.Elements); err != nil {
				return fmt.Errorf("scan keys %q: visit page: %w", pattern, err)
			}
		}

		cursor = entry.Cursor
		if cursor == 0 {
			return nil
		}
	}
}

func (c *Service) Expire(ctx context.Context, key string, ttl time.Duration) error {
	ttlSeconds, err := ttlSecondsCeil(ttl)
	if err != nil {
		return NewCacheError("expire", key, err)
	}

	if err := c.client.Do(ctx, c.client.B().Expire().Key(key).Seconds(ttlSeconds).Build()).Error(); err != nil {
		c.logger.Error("Cache expire failed", privacylog.CacheKeyAttr(key), slog.Any("error", err))

		return NewCacheError("expire", key, err)
	}

	return nil
}

func (c *Service) Exists(ctx context.Context, key string) (bool, error) {
	resp := c.client.Do(ctx, c.client.B().Exists().Key(key).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache exists failed", privacylog.CacheKeyAttr(key), slog.Any("error", resp.Error()))

		return false, NewCacheError("exists", key, resp.Error())
	}

	count, err := resp.AsInt64()
	if err != nil {
		return false, NewCacheError("exists", key, err)
	}

	return count > 0, nil
}

// 성공하면 true, 이미 존재하면 false를 반환합니다.
func (c *Service) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	var cmd valkey.Completed

	if ttl > 0 {
		ttlSeconds, err := ttlSecondsCeil(ttl)
		if err != nil {
			return false, NewCacheError("setnx", key, err)
		}

		cmd = c.client.B().Set().Key(key).Value(value).Nx().ExSeconds(ttlSeconds).Build()
	} else {
		cmd = c.client.B().Set().Key(key).Value(value).Nx().Build()
	}

	resp := c.client.Do(ctx, cmd)
	if util.IsValkeyNil(resp.Error()) {
		return false, nil // 키가 이미 존재 - 락 획득 실패
	}

	if resp.Error() != nil {
		c.logger.Error("Cache setnx failed", privacylog.CacheKeyAttr(key), slog.Any("error", resp.Error()))

		return false, NewCacheError("setnx", key, resp.Error())
	}

	return true, nil
}
