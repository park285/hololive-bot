package cache

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/util"
)

func (c *Service) HSet(ctx context.Context, key, field, value string) error {
	if err := c.client.Do(ctx, c.client.B().Hset().Key(key).FieldValue().FieldValue(field, value).Build()).Error(); err != nil {
		c.logger.Error("Cache hset failed", slog.String("key", key), slog.String("field", field), slog.Any("error", err))
		return NewCacheError("hset failed", "hset", key, err)
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
		c.logger.Error("Cache hmset failed", slog.String("key", key), slog.Int("fields", len(fields)), slog.Any("error", err))
		return NewCacheError("hmset failed", "hmset", key, err)
	}
	return nil
}

func (c *Service) HGet(ctx context.Context, key, field string) (string, error) {
	resp := c.client.Do(ctx, c.client.B().Hget().Key(key).Field(field).Build())
	if util.IsValkeyNil(resp.Error()) {
		return "", nil // 필드가 존재하지 않음 - 에러 아님
	}
	if resp.Error() != nil {
		c.logger.Error("Cache hash get failed", slog.String("key", key), slog.String("field", field), slog.Any("error", resp.Error()))
		return "", NewCacheError("hget failed", "hget", key, resp.Error())
	}

	value, err := resp.ToString()
	if err != nil {
		return "", NewCacheError("hget conversion failed", "hget", key, err)
	}

	return value, nil
}

func (c *Service) BatchHGet(ctx context.Context, key string, fields []string) (map[string]string, error) {
	if len(fields) == 0 {
		return map[string]string{}, nil
	}

	cmds := make([]valkey.Completed, 0, len(fields))
	for _, field := range fields {
		cmds = append(cmds, c.client.B().Hget().Key(key).Field(field).Build())
	}

	results := c.client.DoMulti(ctx, cmds...)
	values := make(map[string]string, len(fields))
	for i, result := range results {
		value, found, err := c.batchHGetValue(key, result)
		if err != nil {
			return values, err
		}
		if found {
			values[fields[i]] = value
		}
	}

	return values, nil
}

func (c *Service) batchHGetValue(key string, result valkey.ValkeyResult) (string, bool, error) {
	if err := result.Error(); err != nil {
		if util.IsValkeyNil(err) {
			return "", false, nil
		}
		c.logger.Error("Cache batch hget failed", slog.String("key", key), slog.Any("error", err))
		return "", false, NewCacheError("batch hget failed", "hget", key, err)
	}

	value, err := result.ToString()
	if err != nil {
		return "", false, NewCacheError("batch hget conversion failed", "hget", key, err)
	}
	return value, value != "", nil
}

func (c *Service) HDel(ctx context.Context, key string, fields ...string) error {
	if len(fields) == 0 {
		return nil
	}
	cmd := c.client.B().Hdel().Key(key).Field(fields...).Build()
	if err := c.client.Do(ctx, cmd).Error(); err != nil {
		c.logger.Error("Cache hdel failed", slog.String("key", key), slog.Int("fields", len(fields)), slog.Any("error", err))
		return NewCacheError("hdel failed", "hdel", key, err)
	}
	return nil
}

func (c *Service) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	resp := c.client.Do(ctx, c.client.B().Hgetall().Key(key).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache hgetall failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return map[string]string{}, NewCacheError("hgetall failed", "hgetall", key, resp.Error())
	}

	values, err := resp.AsStrMap()
	if err != nil {
		return map[string]string{}, NewCacheError("hgetall conversion failed", "hgetall", key, err)
	}

	return values, nil
}
