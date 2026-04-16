// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package cache

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"math"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/park285/llm-kakao-bots/shared-go/pkg/json"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/util"
)

// 기본 Key-Value 외에도 Set, Hash 등 다양한 자료구조 연산을 지원한다.
type Service struct {
	client    valkey.Client
	logger    *slog.Logger
	closeOnce sync.Once
}

type Config struct {
	Host       string
	Port       int
	Password   string
	DB         int
	SocketPath string // UDS 경로 (비어있으면 TCP 사용)
	// DisableCache: valkey-go client-side caching 비활성화 (miniredis/RESP2 호환)
	DisableCache      bool
	ForceSingleClient bool
}

// SocketPath가 설정되면 UDS로 연결하고, 비어있으면 TCP로 연결합니다.
func NewCacheService(ctx context.Context, cfg Config, logger *slog.Logger) (*Service, error) {
	var addr string
	var connMethod string
	if cfg.SocketPath != "" {
		addr = cfg.SocketPath
		connMethod = "unix"
	} else {
		addr = fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
		connMethod = "tcp"
	}

	opts := valkey.ClientOption{
		InitAddress:       []string{addr},
		Password:          cfg.Password,
		SelectDB:          cfg.DB,
		ConnWriteTimeout:  constants.MQConfig.ConnWriteTimeout,
		BlockingPoolSize:  constants.ValkeyConfig.BlockingPoolSize,
		PipelineMultiplex: constants.ValkeyConfig.PipelineMultiplex,
		DisableCache:      cfg.DisableCache,
		ForceSingleClient: cfg.ForceSingleClient,
	}

	if cfg.SocketPath != "" {
		socketPath := cfg.SocketPath
		opts.DialCtxFn = func(ctx context.Context, _ string, _ *net.Dialer, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			d.Timeout = constants.MQConfig.DialTimeout
			return d.DialContext(ctx, "unix", socketPath)
		}
	} else {
		opts.Dialer = net.Dialer{Timeout: constants.MQConfig.DialTimeout}
	}

	client, err := valkey.NewClient(opts)
	if err != nil {
		return nil, NewCacheError("failed to create cache client", "init", "", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, constants.ValkeyConfig.ReadyTimeout)
	defer cancel()

	if err := client.Do(pingCtx, client.B().Ping().Build()).Error(); err != nil {
		client.Close()
		return nil, NewCacheError("failed to connect to cache store", "ping", "", err)
	}

	logger.Info("Cache store connected",
		slog.String("addr", addr),
		slog.String("method", connMethod),
		slog.Int("db", cfg.DB),
		slog.Int("pool_size", constants.ValkeyConfig.BlockingPoolSize),
	)

	return &Service{
		client: client,
		logger: logger,
	}, nil
}

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

	resp := c.client.Do(ctx, c.client.B().Del().Key(keys...).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache delete many failed", slog.Int("count", len(keys)), slog.Any("error", resp.Error()))
		return 0, NewCacheError("delete many failed", "del", fmt.Sprintf("%d keys", len(keys)), resp.Error())
	}

	deleted, err := resp.AsInt64()
	if err != nil {
		return 0, NewCacheError("delete many conversion failed", "del", "", err)
	}

	return deleted, nil
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

func (c *Service) SAdd(ctx context.Context, key string, members []string) (int64, error) {
	if len(members) == 0 {
		return 0, nil
	}

	resp := c.client.Do(ctx, c.client.B().Sadd().Key(key).Member(members...).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache sadd failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return 0, NewCacheError("sadd failed", "sadd", key, resp.Error())
	}

	added, err := resp.AsInt64()
	if err != nil {
		return 0, NewCacheError("sadd conversion failed", "sadd", key, err)
	}

	return added, nil
}

func (c *Service) SRem(ctx context.Context, key string, members []string) (int64, error) {
	if len(members) == 0 {
		return 0, nil
	}

	resp := c.client.Do(ctx, c.client.B().Srem().Key(key).Member(members...).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache srem failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return 0, NewCacheError("srem failed", "srem", key, resp.Error())
	}

	removed, err := resp.AsInt64()
	if err != nil {
		return 0, NewCacheError("srem conversion failed", "srem", key, err)
	}

	return removed, nil
}

func (c *Service) SMembers(ctx context.Context, key string) ([]string, error) {
	resp := c.client.Do(ctx, c.client.B().Smembers().Key(key).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache smembers failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return []string{}, NewCacheError("smembers failed", "smembers", key, resp.Error())
	}

	members, err := resp.AsStrSlice()
	if err != nil {
		return []string{}, NewCacheError("smembers conversion failed", "smembers", key, err)
	}

	return members, nil
}

func (c *Service) SIsMember(ctx context.Context, key, member string) (bool, error) {
	resp := c.client.Do(ctx, c.client.B().Sismember().Key(key).Member(member).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache sismember failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return false, NewCacheError("sismember failed", "sismember", key, resp.Error())
	}

	exists, err := resp.AsBool()
	if err != nil {
		return false, NewCacheError("sismember conversion failed", "sismember", key, err)
	}

	return exists, nil
}

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

func (c *Service) Close() error {
	var closeErr error

	c.closeOnce.Do(func() {
		if c.client == nil {
			return
		}

		c.client.Close()
		c.logger.Info("Cache store disconnected")
	})

	return closeErr
}

func (c *Service) IsConnected(ctx context.Context) bool {
	return c.client.Do(ctx, c.client.B().Ping().Build()).Error() == nil
}

func (c *Service) WaitUntilReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for cache store to be ready")
		case <-ticker.C:
			if c.IsConnected(ctx) {
				return nil
			}
		}
	}
}

func (c *Service) GetClient() valkey.Client {
	return c.client
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

func (c *Service) DoMulti(ctx context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult {
	if len(cmds) == 0 {
		return nil
	}
	return c.client.DoMulti(ctx, cmds...)
}

func (c *Service) Builder() valkey.Builder {
	return c.client.B()
}

// B: 명령 빌더를 반환합니다.
func (c *Service) B() valkey.Builder {
	return c.client.B()
}

// compareAndDeleteScript: 원자적 compare-and-delete Lua 스크립트
const compareAndDeleteScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
else
  return 0
end`

// 분산 락의 안전한 해제에 사용됩니다.
func (c *Service) CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error) {
	cmd := c.client.B().Eval().Script(compareAndDeleteScript).Numkeys(1).Key(key).Arg(expectedValue).Build()
	resp := c.client.Do(ctx, cmd)
	if resp.Error() != nil {
		c.logger.Error("Cache compare-and-delete failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return false, NewCacheError("compare-and-delete failed", "cas", key, resp.Error())
	}

	result, err := resp.AsInt64()
	if err != nil {
		return false, NewCacheError("compare-and-delete conversion failed", "cas", key, err)
	}

	return result == 1, nil
}

// compareAndExpireScript: 원자적 compare-and-expire Lua 스크립트
const compareAndExpireScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('EXPIRE', KEYS[1], ARGV[2])
else
  return 0
end`

// 분산 락 renew 시 소유권 보장을 위해 사용됩니다.
func (c *Service) CompareAndExpire(ctx context.Context, key, expectedValue string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, fmt.Errorf("compare-and-expire: ttl must be greater than zero")
	}

	ttlSeconds := int64(math.Ceil(ttl.Seconds()))
	if ttlSeconds <= 0 {
		return false, fmt.Errorf("compare-and-expire: ttl seconds must be greater than zero")
	}

	cmd := c.client.B().Eval().
		Script(compareAndExpireScript).
		Numkeys(1).
		Key(key).
		Arg(expectedValue, strconv.FormatInt(ttlSeconds, 10)).
		Build()
	resp := c.client.Do(ctx, cmd)
	if resp.Error() != nil {
		c.logger.Error("Cache compare-and-expire failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return false, NewCacheError("compare-and-expire failed", "cas-expire", key, resp.Error())
	}

	result, err := resp.AsInt64()
	if err != nil {
		return false, NewCacheError("compare-and-expire conversion failed", "cas-expire", key, err)
	}

	return result == 1, nil
}
