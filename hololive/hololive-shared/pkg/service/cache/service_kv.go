package cache

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/util"
)

func (c *Service) Get(ctx context.Context, key string, dest any) error {
	resp := c.client.Do(ctx, c.client.B().Get().Key(key).Build())
	if util.IsValkeyNil(resp.Error()) {
		return nil // 키가 존재하지 않음 - 에러 아님
	}
	if resp.Error() != nil {
		c.logger.Error("Cache get operation failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return NewCacheError("get failed", "get", key, resp.Error())
	}

	value, err := resp.ToString()
	if err != nil {
		c.logger.Error("Cache value conversion failed", slog.String("key", key), slog.Any("error", err))
		return NewCacheError("conversion failed", "get", key, err)
	}

	if dest != nil {
		if err := json.Unmarshal([]byte(value), dest); err != nil {
			c.logger.Error("Cache value unmarshal failed", slog.String("key", key), slog.Any("error", err))
			return NewCacheError("unmarshal failed", "get", key, err)
		}
	}

	return nil
}

// MGet 배치 조회 (파이프라이닝 활용)
func (c *Service) MGet(ctx context.Context, keys []string) (map[string]string, error) {
	if len(keys) == 0 {
		return make(map[string]string), nil
	}

	resp := c.client.Do(ctx, c.client.B().Mget().Key(keys...).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache mget failed", slog.Int("keys", len(keys)), slog.Any("error", resp.Error()))
		return nil, NewCacheError("mget failed", "mget", fmt.Sprintf("%d keys", len(keys)), resp.Error())
	}

	values, err := resp.AsStrSlice()
	if err != nil {
		return nil, NewCacheError("mget conversion failed", "mget", "", err)
	}

	result := make(map[string]string, len(keys))
	for i, key := range keys {
		if i < len(values) && values[i] != "" {
			result[key] = values[i]
		}
	}

	return result, nil
}

func ttlSecondsCeil(ttl time.Duration) (int64, error) {
	if ttl < 0 {
		return 0, fmt.Errorf("ttl must not be negative")
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
	jsonData, err := json.Marshal(value)
	if err != nil {
		return NewCacheError("marshal failed", "set", key, err)
	}

	var cmd valkey.Completed
	if ttl > 0 {
		ttlSeconds, err := ttlSecondsCeil(ttl)
		if err != nil {
			return NewCacheError("invalid ttl", "set", key, err)
		}
		cmd = c.client.B().Set().Key(key).Value(string(jsonData)).ExSeconds(ttlSeconds).Build()
	} else {
		cmd = c.client.B().Set().Key(key).Value(string(jsonData)).Build()
	}

	if err := c.client.Do(ctx, cmd).Error(); err != nil {
		c.logger.Error("Cache set failed", slog.String("key", key), slog.Any("error", err))
		return NewCacheError("set failed", "set", key, err)
	}

	return nil
}

// MSet 배치 저장 (파이프라이닝 활용)
func (c *Service) MSet(ctx context.Context, pairs map[string]any, ttl time.Duration) error {
	if len(pairs) == 0 {
		return nil
	}

	// 파이프라인 사용
	cmds := make([]valkey.Completed, 0, len(pairs))
	for key, value := range pairs {
		jsonData, err := json.Marshal(value)
		if err != nil {
			c.logger.Error("Failed to marshal value for MSet", slog.String("key", key), slog.Any("error", err))
			return NewCacheError("marshal failed", "mset", key, err)
		}

		var cmd valkey.Completed
		if ttl > 0 {
			ttlSeconds, err := ttlSecondsCeil(ttl)
			if err != nil {
				return NewCacheError("invalid ttl", "mset", key, err)
			}
			cmd = c.client.B().Set().Key(key).Value(string(jsonData)).ExSeconds(ttlSeconds).Build()
		} else {
			cmd = c.client.B().Set().Key(key).Value(string(jsonData)).Build()
		}
		cmds = append(cmds, cmd)
	}

	// 배치 실행
	for _, resp := range c.client.DoMulti(ctx, cmds...) {
		if resp.Error() != nil {
			c.logger.Error("MSet command failed", slog.Any("error", resp.Error()))
			return NewCacheError("mset failed", "mset", "", resp.Error())
		}
	}

	return nil
}

func (c *Service) Del(ctx context.Context, key string) error {
	if err := c.client.Do(ctx, c.client.B().Del().Key(key).Build()).Error(); err != nil {
		c.logger.Error("Cache delete failed", slog.String("key", key), slog.Any("error", err))
		return NewCacheError("delete failed", "del", key, err)
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
		end := start + delManyChunkSize
		if end > len(keys) {
			end = len(keys)
		}

		chunk := keys[start:end]
		resp := c.client.Do(ctx, c.client.B().Del().Key(chunk...).Build())
		if resp.Error() != nil {
			c.logger.Error("Cache delete many failed", slog.Int("count", len(chunk)), slog.Any("error", resp.Error()))
			return totalDeleted, NewCacheError("delete many failed", "del", fmt.Sprintf("%d keys", len(chunk)), resp.Error())
		}

		deleted, err := resp.AsInt64()
		if err != nil {
			return totalDeleted, NewCacheError("delete many conversion failed", "del", "", err)
		}
		totalDeleted += deleted
	}

	return totalDeleted, nil
}

// KEYS와 달리 Redis를 블로킹하지 않아 대량 키 조회에 안전하다.
// 단, 비원자적이므로 스캔 중 키 변경 시 누락/중복이 발생할 수 있다.
func (c *Service) ScanKeys(ctx context.Context, pattern string, batchSize int64) ([]string, error) {
	if batchSize <= 0 {
		batchSize = 100
	}

	var keys []string
	cursor := uint64(0)

	for {
		cmd := c.client.B().Scan().Cursor(cursor).Match(pattern).Count(batchSize).Build()
		resp := c.client.Do(ctx, cmd)
		if resp.Error() != nil {
			c.logger.Error("Cache scan failed", slog.String("pattern", pattern), slog.Any("error", resp.Error()))
			return keys, NewCacheError("scan failed", "scan", pattern, resp.Error())
		}

		entry, err := resp.AsScanEntry()
		if err != nil {
			return keys, NewCacheError("scan parse failed", "scan", pattern, err)
		}

		keys = append(keys, entry.Elements...)
		cursor = entry.Cursor

		if cursor == 0 {
			break
		}
	}

	return keys, nil
}

func (c *Service) Expire(ctx context.Context, key string, ttl time.Duration) error {
	ttlSeconds, err := ttlSecondsCeil(ttl)
	if err != nil {
		return NewCacheError("invalid ttl", "expire", key, err)
	}
	if err := c.client.Do(ctx, c.client.B().Expire().Key(key).Seconds(ttlSeconds).Build()).Error(); err != nil {
		c.logger.Error("Cache expire failed", slog.String("key", key), slog.Any("error", err))
		return NewCacheError("expire failed", "expire", key, err)
	}
	return nil
}

func (c *Service) Exists(ctx context.Context, key string) (bool, error) {
	resp := c.client.Do(ctx, c.client.B().Exists().Key(key).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache exists failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return false, NewCacheError("exists failed", "exists", key, resp.Error())
	}

	count, err := resp.AsInt64()
	if err != nil {
		return false, NewCacheError("exists conversion failed", "exists", key, err)
	}

	return count > 0, nil
}

// 성공하면 true, 이미 존재하면 false를 반환합니다.
func (c *Service) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	var cmd valkey.Completed
	if ttl > 0 {
		ttlSeconds, err := ttlSecondsCeil(ttl)
		if err != nil {
			return false, NewCacheError("invalid ttl", "setnx", key, err)
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
		c.logger.Error("Cache setnx failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return false, NewCacheError("setnx failed", "setnx", key, resp.Error())
	}

	return true, nil
}
