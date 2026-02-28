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
	"github.com/kapu/hololive-shared/pkg/errors"
)

// Service: Valkey(Redis) 클라이언트를 래핑하여 캐싱 기능을 제공하는 서비스
// 기본 Key-Value 외에도 Set, Hash 등 다양한 자료구조 연산을 지원한다.
type Service struct {
	client    valkey.Client
	logger    *slog.Logger
	closeOnce sync.Once
}

// Config: Valkey 연결 설정을 담는 구조체
type Config struct {
	Host       string
	Port       int
	Password   string
	DB         int
	SocketPath string // UDS 경로 (비어있으면 TCP 사용)
	// DisableCache: valkey-go client-side caching 비활성화 (miniredis/RESP2 호환)
	DisableCache bool
}

// NewCacheService: 새로운 Valkey 캐시 서비스 인스턴스를 생성하고 연결을 수립합니다.
// SocketPath가 설정되면 UDS로 연결하고, 비어있으면 TCP로 연결합니다.
func NewCacheService(ctx context.Context, cfg Config, logger *slog.Logger) (*Service, error) {
	// 주소 결정: UDS 모드면 소켓 경로, TCP 모드면 host:port
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
	}

	// UDS 모드: 커스텀 DialCtxFn 설정
	if cfg.SocketPath != "" {
		socketPath := cfg.SocketPath
		opts.DialCtxFn = func(ctx context.Context, _ string, _ *net.Dialer, _ *tls.Config) (net.Conn, error) {
			var d net.Dialer
			d.Timeout = constants.MQConfig.DialTimeout
			return d.DialContext(ctx, "unix", socketPath)
		}
	} else {
		// TCP 모드: Dialer 설정
		opts.Dialer = net.Dialer{Timeout: constants.MQConfig.DialTimeout}
	}

	client, err := valkey.NewClient(opts)
	if err != nil {
		return nil, errors.NewCacheError("failed to create cache client", "init", "", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, constants.ValkeyConfig.ReadyTimeout)
	defer cancel()

	// Ping 테스트
	if err := client.Do(pingCtx, client.B().Ping().Build()).Error(); err != nil {
		client.Close()
		return nil, errors.NewCacheError("failed to connect to cache store", "ping", "", err)
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

// Get: 키에 해당하는 값을 조회하고, 결과를 dest 인터페이스에 언마샬링합니다.
func (c *Service) Get(ctx context.Context, key string, dest any) error {
	resp := c.client.Do(ctx, c.client.B().Get().Key(key).Build())
	if util.IsValkeyNil(resp.Error()) {
		return nil // 키가 존재하지 않음 - 에러 아님
	}
	if resp.Error() != nil {
		c.logger.Error("Cache get operation failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return errors.NewCacheError("get failed", "get", key, resp.Error())
	}

	value, err := resp.ToString()
	if err != nil {
		c.logger.Error("Cache value conversion failed", slog.String("key", key), slog.Any("error", err))
		return errors.NewCacheError("conversion failed", "get", key, err)
	}

	if dest != nil {
		if err := json.Unmarshal([]byte(value), dest); err != nil {
			c.logger.Error("Cache value unmarshal failed", slog.String("key", key), slog.Any("error", err))
			return errors.NewCacheError("unmarshal failed", "get", key, err)
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
		return nil, errors.NewCacheError("mget failed", "mget", fmt.Sprintf("%d keys", len(keys)), resp.Error())
	}

	values, err := resp.AsStrSlice()
	if err != nil {
		return nil, errors.NewCacheError("mget conversion failed", "mget", "", err)
	}

	result := make(map[string]string, len(keys))
	for i, key := range keys {
		if i < len(values) && values[i] != "" {
			result[key] = values[i]
		}
	}

	return result, nil
}

// Set: 값을 JSON으로 마샬링하여 키에 저장한다. (TTL 지정 가능)
func (c *Service) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	jsonData, err := json.Marshal(value)
	if err != nil {
		return errors.NewCacheError("marshal failed", "set", key, err)
	}

	var cmd valkey.Completed
	if ttl > 0 {
		cmd = c.client.B().Set().Key(key).Value(string(jsonData)).ExSeconds(int64(ttl.Seconds())).Build()
	} else {
		cmd = c.client.B().Set().Key(key).Value(string(jsonData)).Build()
	}

	if err := c.client.Do(ctx, cmd).Error(); err != nil {
		c.logger.Error("Cache set failed", slog.String("key", key), slog.Any("error", err))
		return errors.NewCacheError("set failed", "set", key, err)
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
			c.logger.Warn("Failed to marshal value for MSet", slog.String("key", key), slog.Any("error", err))
			continue
		}

		var cmd valkey.Completed
		if ttl > 0 {
			cmd = c.client.B().Set().Key(key).Value(string(jsonData)).ExSeconds(int64(ttl.Seconds())).Build()
		} else {
			cmd = c.client.B().Set().Key(key).Value(string(jsonData)).Build()
		}
		cmds = append(cmds, cmd)
	}

	// 배치 실행
	for _, resp := range c.client.DoMulti(ctx, cmds...) {
		if resp.Error() != nil {
			c.logger.Error("MSet command failed", slog.Any("error", resp.Error()))
			return errors.NewCacheError("mset failed", "mset", "", resp.Error())
		}
	}

	return nil
}

// Del: 지정된 키를 삭제합니다.
func (c *Service) Del(ctx context.Context, key string) error {
	if err := c.client.Do(ctx, c.client.B().Del().Key(key).Build()).Error(); err != nil {
		c.logger.Error("Cache delete failed", slog.String("key", key), slog.Any("error", err))
		return errors.NewCacheError("delete failed", "del", key, err)
	}
	return nil
}

// DelMany: 여러 키를 한 번에 삭제합니다.
func (c *Service) DelMany(ctx context.Context, keys []string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}

	resp := c.client.Do(ctx, c.client.B().Del().Key(keys...).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache delete many failed", slog.Int("count", len(keys)), slog.Any("error", resp.Error()))
		return 0, errors.NewCacheError("delete many failed", "del", fmt.Sprintf("%d keys", len(keys)), resp.Error())
	}

	deleted, err := resp.AsInt64()
	if err != nil {
		return 0, errors.NewCacheError("delete many conversion failed", "del", "", err)
	}

	return deleted, nil
}

// ScanKeys: SCAN 명령을 사용하여 패턴과 일치하는 키를 점진적으로 조회합니다.
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
			return keys, errors.NewCacheError("scan failed", "scan", pattern, resp.Error())
		}

		entry, err := resp.AsScanEntry()
		if err != nil {
			return keys, errors.NewCacheError("scan parse failed", "scan", pattern, err)
		}

		keys = append(keys, entry.Elements...)
		cursor = entry.Cursor

		if cursor == 0 {
			break
		}
	}

	return keys, nil
}

// SAdd: Set 자료구조에 멤버들을 추가합니다.
func (c *Service) SAdd(ctx context.Context, key string, members []string) (int64, error) {
	if len(members) == 0 {
		return 0, nil
	}

	resp := c.client.Do(ctx, c.client.B().Sadd().Key(key).Member(members...).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache sadd failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return 0, errors.NewCacheError("sadd failed", "sadd", key, resp.Error())
	}

	added, err := resp.AsInt64()
	if err != nil {
		return 0, errors.NewCacheError("sadd conversion failed", "sadd", key, err)
	}

	return added, nil
}

// SRem: Set 자료구조에서 멤버들을 제거합니다.
func (c *Service) SRem(ctx context.Context, key string, members []string) (int64, error) {
	if len(members) == 0 {
		return 0, nil
	}

	resp := c.client.Do(ctx, c.client.B().Srem().Key(key).Member(members...).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache srem failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return 0, errors.NewCacheError("srem failed", "srem", key, resp.Error())
	}

	removed, err := resp.AsInt64()
	if err != nil {
		return 0, errors.NewCacheError("srem conversion failed", "srem", key, err)
	}

	return removed, nil
}

// SMembers: Set의 모든 멤버를 조회합니다.
func (c *Service) SMembers(ctx context.Context, key string) ([]string, error) {
	resp := c.client.Do(ctx, c.client.B().Smembers().Key(key).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache smembers failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return []string{}, errors.NewCacheError("smembers failed", "smembers", key, resp.Error())
	}

	members, err := resp.AsStrSlice()
	if err != nil {
		return []string{}, errors.NewCacheError("smembers conversion failed", "smembers", key, err)
	}

	return members, nil
}

// SIsMember: 특정 값이 Set에 포함되어 있는지 확인합니다.
func (c *Service) SIsMember(ctx context.Context, key, member string) (bool, error) {
	resp := c.client.Do(ctx, c.client.B().Sismember().Key(key).Member(member).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache sismember failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return false, errors.NewCacheError("sismember failed", "sismember", key, resp.Error())
	}

	exists, err := resp.AsBool()
	if err != nil {
		return false, errors.NewCacheError("sismember conversion failed", "sismember", key, err)
	}

	return exists, nil
}

// HSet: Hash 자료구조의 특정 필드에 값을 설정합니다.
func (c *Service) HSet(ctx context.Context, key, field, value string) error {
	if err := c.client.Do(ctx, c.client.B().Hset().Key(key).FieldValue().FieldValue(field, value).Build()).Error(); err != nil {
		c.logger.Error("Cache hset failed", slog.String("key", key), slog.String("field", field), slog.Any("error", err))
		return errors.NewCacheError("hset failed", "hset", key, err)
	}
	return nil
}

// HMSet: Hash 자료구조에 여러 필드와 값을 한 번에 설정합니다.
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
		return errors.NewCacheError("hmset failed", "hmset", key, err)
	}
	return nil
}

// HGet: Hash 자료구조에서 특정 필드의 값을 조회합니다.
func (c *Service) HGet(ctx context.Context, key, field string) (string, error) {
	resp := c.client.Do(ctx, c.client.B().Hget().Key(key).Field(field).Build())
	if util.IsValkeyNil(resp.Error()) {
		return "", nil // 필드가 존재하지 않음 - 에러 아님
	}
	if resp.Error() != nil {
		c.logger.Error("Cache hash get failed", slog.String("key", key), slog.String("field", field), slog.Any("error", resp.Error()))
		return "", errors.NewCacheError("hget failed", "hget", key, resp.Error())
	}

	value, err := resp.ToString()
	if err != nil {
		return "", errors.NewCacheError("hget conversion failed", "hget", key, err)
	}

	return value, nil
}

// HDel: Hash 자료구조에서 특정 필드를 삭제합니다.
func (c *Service) HDel(ctx context.Context, key string, fields ...string) error {
	if len(fields) == 0 {
		return nil
	}
	cmd := c.client.B().Hdel().Key(key).Field(fields...).Build()
	if err := c.client.Do(ctx, cmd).Error(); err != nil {
		c.logger.Error("Cache hdel failed", slog.String("key", key), slog.Int("fields", len(fields)), slog.Any("error", err))
		return errors.NewCacheError("hdel failed", "hdel", key, err)
	}
	return nil
}

// HGetAll: Hash의 모든 필드와 값을 조회합니다.
func (c *Service) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	resp := c.client.Do(ctx, c.client.B().Hgetall().Key(key).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache hgetall failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return map[string]string{}, errors.NewCacheError("hgetall failed", "hgetall", key, resp.Error())
	}

	values, err := resp.AsStrMap()
	if err != nil {
		return map[string]string{}, errors.NewCacheError("hgetall conversion failed", "hgetall", key, err)
	}

	return values, nil
}

// Expire: 키의 만료 시간을 설정합니다.
func (c *Service) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if err := c.client.Do(ctx, c.client.B().Expire().Key(key).Seconds(int64(ttl.Seconds())).Build()).Error(); err != nil {
		c.logger.Error("Cache expire failed", slog.String("key", key), slog.Any("error", err))
		return errors.NewCacheError("expire failed", "expire", key, err)
	}
	return nil
}

// Exists: 키가 존재하는지 확인합니다.
func (c *Service) Exists(ctx context.Context, key string) (bool, error) {
	resp := c.client.Do(ctx, c.client.B().Exists().Key(key).Build())
	if resp.Error() != nil {
		c.logger.Error("Cache exists failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return false, errors.NewCacheError("exists failed", "exists", key, resp.Error())
	}

	count, err := resp.AsInt64()
	if err != nil {
		return false, errors.NewCacheError("exists conversion failed", "exists", key, err)
	}

	return count > 0, nil
}

// Close: 캐시 스토어 연결을 안전하게 종료합니다.
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

// IsConnected: 캐시 스토어와 연결되어 있는지(PING 응답 여부) 확인합니다.
func (c *Service) IsConnected(ctx context.Context) bool {
	return c.client.Do(ctx, c.client.B().Ping().Build()).Error() == nil
}

// WaitUntilReady: 캐시 스토어 연결이 완료될 때까지 대기한다. (타임아웃 적용)
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

// GetClient: 고급 조작을 위해 내부 Valkey 클라이언트를 반환합니다.
func (c *Service) GetClient() valkey.Client {
	return c.client
}

// SetNX: 키가 존재하지 않을 때만 값을 설정합니다 (분산 락 용도).
// 성공하면 true, 이미 존재하면 false를 반환합니다.
func (c *Service) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	var cmd valkey.Completed
	if ttl > 0 {
		cmd = c.client.B().Set().Key(key).Value(value).Nx().ExSeconds(int64(ttl.Seconds())).Build()
	} else {
		cmd = c.client.B().Set().Key(key).Value(value).Nx().Build()
	}

	resp := c.client.Do(ctx, cmd)
	if util.IsValkeyNil(resp.Error()) {
		return false, nil // 키가 이미 존재 - 락 획득 실패
	}
	if resp.Error() != nil {
		c.logger.Error("Cache setnx failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return false, errors.NewCacheError("setnx failed", "setnx", key, resp.Error())
	}

	return true, nil
}

// DoMulti: 여러 Valkey 명령을 파이프라인으로 배치 실행합니다.
func (c *Service) DoMulti(ctx context.Context, cmds ...valkey.Completed) []valkey.ValkeyResult {
	if len(cmds) == 0 {
		return nil
	}
	return c.client.DoMulti(ctx, cmds...)
}

// Builder: Valkey 명령 빌더를 반환합니다. 파이프라인 작업에 사용됩니다.
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

// CompareAndDelete: 키의 값이 expectedValue와 일치할 때만 삭제합니다 (원자적 CAS).
// 분산 락의 안전한 해제에 사용됩니다.
func (c *Service) CompareAndDelete(ctx context.Context, key, expectedValue string) (bool, error) {
	cmd := c.client.B().Eval().Script(compareAndDeleteScript).Numkeys(1).Key(key).Arg(expectedValue).Build()
	resp := c.client.Do(ctx, cmd)
	if resp.Error() != nil {
		c.logger.Error("Cache compare-and-delete failed", slog.String("key", key), slog.Any("error", resp.Error()))
		return false, errors.NewCacheError("compare-and-delete failed", "cas", key, resp.Error())
	}

	result, err := resp.AsInt64()
	if err != nil {
		return false, errors.NewCacheError("compare-and-delete conversion failed", "cas", key, err)
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

// CompareAndExpire: 키의 값이 expectedValue와 일치할 때만 TTL을 갱신합니다 (원자적 CAS).
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
		return false, errors.NewCacheError("compare-and-expire failed", "cas-expire", key, resp.Error())
	}

	result, err := resp.AsInt64()
	if err != nil {
		return false, errors.NewCacheError("compare-and-expire conversion failed", "cas-expire", key, err)
	}

	return result == 1, nil
}
