package cache

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/privacylog"
)

func (c *Service) HSet(ctx context.Context, key, field, value string) error {
	if err := c.client.Do(ctx, c.client.B().Hset().Key(key).FieldValue().FieldValue(field, value).Build()).Error(); err != nil {
		c.logger.Error("Cache hset failed", privacylog.CacheKeyAttr(key), privacylog.CacheFieldAttr(key, field), slog.Any("error", err))

		return NewCacheError("hset", key, err)
	}

	return nil
}

func (c *Service) HMSet(ctx context.Context, key string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}

	builder := c.client.B().Hset().Key(key).FieldValue()

	for field, value := range fields {
		builder = builder.FieldValue(field, fmt.Sprintf("%v", value))
	}

	if err := c.client.Do(ctx, builder.Build()).Error(); err != nil {
		c.logger.Error("Cache hmset failed", privacylog.CacheKeyAttr(key), slog.Int("fields", len(fields)), slog.Any("error", err))

		return NewCacheError("hmset", key, err)
	}

	return nil
}

func (c *Service) HGet(ctx context.Context, key, field string) (string, error) {
	resp := c.client.Do(ctx, c.client.B().Hget().Key(key).Field(field).Build())
	if errors.Is(resp.Error(), valkey.Nil) {
		return "", nil // 필드가 존재하지 않음 - 에러 아님
	}

	if resp.Error() != nil {
		c.logger.Error("Cache hash get failed", privacylog.CacheKeyAttr(key), privacylog.CacheFieldAttr(key, field), slog.Any("error", resp.Error()))

		return "", NewCacheError("hget", key, resp.Error())
	}

	value, err := resp.ToString()
	if err != nil {
		return "", NewCacheError("hget", key, err)
	}

	return value, nil
}

func (c *Service) BatchHGet(ctx context.Context, key string, fields []string) (map[string]string, error) {
	if len(fields) == 0 {
		return map[string]string{}, nil
	}

	// 필드 수만큼 HGET을 보내는 대신 HMGET 한 명령으로 받는다. 없는 필드는 nil 원소로 온다.
	resp := c.client.Do(ctx, c.client.B().Hmget().Key(key).Field(fields...).Build())
	if err := resp.Error(); err != nil {
		c.logger.Error("Cache batch hget failed", privacylog.CacheKeyAttr(key), slog.Any("error", err))

		return nil, NewCacheError("hmget", key, err)
	}

	items, err := resp.ToArray()
	if err != nil {
		return nil, NewCacheError("hmget", key, err)
	}

	if len(items) != len(fields) {
		return nil, NewCacheError("hmget", key, fmt.Errorf("hmget returned %d values for %d fields", len(items), len(fields)))
	}

	values := make(map[string]string, len(fields))

	for i := range items {
		if items[i].IsNil() {
			continue
		}

		value, err := items[i].ToString()
		if err != nil {
			return nil, NewCacheError("hmget", key, err)
		}

		if value != "" {
			values[fields[i]] = value
		}
	}

	return values, nil
}

func (c *Service) HDel(ctx context.Context, key string, fields ...string) error {
	if len(fields) == 0 {
		return nil
	}

	cmd := c.client.B().Hdel().Key(key).Field(fields...).Build()
	if err := c.client.Do(ctx, cmd).Error(); err != nil {
		c.logger.Error("Cache hdel failed", privacylog.CacheKeyAttr(key), slog.Int("fields", len(fields)), slog.Any("error", err))

		return NewCacheError("hdel", key, err)
	}

	return nil
}

func (c *Service) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	resp := c.client.Do(ctx, c.client.B().Hgetall().Key(key).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache hgetall failed", privacylog.CacheKeyAttr(key), slog.Any("error", resp.Error()))

		return nil, NewCacheError("hgetall", key, resp.Error())
	}

	values, err := resp.AsStrMap()
	if err != nil {
		return nil, NewCacheError("hgetall", key, err)
	}

	return values, nil
}
