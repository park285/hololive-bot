package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"
)

const (
	loginLimitWindow         = 15 * time.Minute
	loginIPFailureLimit      = 10
	loginAccountFailureLimit = 30
	loginGlobalFailureLimit  = 200
)

// LoginLimiter는 IP·계정·전체 로그인 실패 예산을 Valkey에서 공유합니다.
type LoginLimiter struct {
	client valkey.Client
}

// NewLoginLimiter는 전용 client를 연결하고 ping이 성공해야 반환합니다. 호출자가 Close를 소유합니다.
func NewLoginLimiter(ctx context.Context, valkeyURL string) (*LoginLimiter, error) {
	addr, password, err := parseLoginLimiterValkeyAddress(valkeyURL)
	if err != nil {
		return nil, fmt.Errorf("parse login limiter valkey address: %w", err)
	}

	// 작은 정수 응답을 받는 전용 연결도 세션 저장소와 같은 버퍼 크기를 사용합니다.
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:         []string{addr},
		Password:            password,
		DisableCache:        true,
		ForceSingleClient:   true,
		PipelineMultiplex:   valkeyPipelineMultiplex,
		BlockingPoolSize:    32,
		ReadBufferEachConn:  valkeyConnectionBufferSize,
		WriteBufferEachConn: valkeyConnectionBufferSize,
		Dialer:              net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second},
		ConnWriteTimeout:    3 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("create login limiter valkey client: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)

	defer cancel()

	if err := client.Do(pingCtx, client.B().Ping().Build()).Error(); err != nil {
		client.Close()

		return nil, fmt.Errorf("login limiter valkey ping failed: %w", err)
	}

	return &LoginLimiter{client: client}, nil
}

// Close는 전용 Valkey client를 닫습니다.
func (l *LoginLimiter) Close() {
	if l != nil && l.client != nil {
		l.client.Close()
	}
}

// Check는 IP·계정·전체 실패 예산 중 가장 긴 거부 시간을 반환하며 store 부재·실패는 오류입니다.
func (l *LoginLimiter) Check(ctx context.Context, ip, account string) (time.Duration, error) {
	if l == nil || l.client == nil {
		return 0, errors.New("login limiter is unavailable")
	}

	keys := loginLimiterKeys(ip, account)

	result, err := l.evalInt(ctx, loginLimiterCheckScript, keys, []string{
		fmt.Sprint(loginIPFailureLimit),
		fmt.Sprint(loginAccountFailureLimit),
		fmt.Sprint(loginGlobalFailureLimit),
	})
	if err != nil {
		return 0, fmt.Errorf("eval int: %w", err)
	}

	if result <= 0 {
		return 0, nil
	}

	return time.Duration(result) * time.Second, nil
}

// RecordFailure는 세 실패 예산을 원자적으로 증가시키고 만료를 설정합니다.
func (l *LoginLimiter) RecordFailure(ctx context.Context, ip, account string) (int, error) {
	if l == nil || l.client == nil {
		return 0, errors.New("login limiter is unavailable")
	}

	result, err := l.evalInt(ctx, loginLimiterFailureScript, loginLimiterKeys(ip, account), []string{
		fmt.Sprint(int(loginLimitWindow.Seconds())),
	})
	if err != nil {
		return 0, fmt.Errorf("eval int: %w", err)
	}

	return int(result), nil
}

// RecordSuccess는 인증 성공 시 IP·계정 실패만 해제하고 전체 예산은 유지합니다.
func (l *LoginLimiter) RecordSuccess(ctx context.Context, ip, account string) error {
	if l == nil || l.client == nil {
		return errors.New("login limiter is unavailable")
	}

	keys := loginLimiterKeys(ip, account)
	if _, err := l.evalInt(ctx, loginLimiterSuccessScript, keys[:2], nil); err != nil {
		return fmt.Errorf("record login success: %w", err)
	}

	return nil
}

func (l *LoginLimiter) evalInt(ctx context.Context, script string, keys, args []string) (int64, error) {
	cmd := l.client.B().Eval().Script(script).Numkeys(int64(len(keys))).Key(keys...).Arg(args...).Build()
	resp := l.client.Do(ctx, cmd)

	if err := resp.Error(); err != nil {
		return 0, fmt.Errorf("error: %w", err)
	}

	out, err := resp.AsInt64()
	if err != nil {
		return out, fmt.Errorf("as int64: %w", err)
	}

	return out, nil
}

func loginLimiterKeys(ip, account string) []string {
	return []string{
		"login:admin:limit:ip:" + loginBucketHash(ip),
		"login:admin:limit:account:" + loginBucketHash(account),
		"login:admin:limit:global",
	}
}

func loginBucketHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

func parseLoginLimiterValkeyAddress(value string) (addr, password string, err error) {
	userinfo, host, ok := strings.Cut(value, "@")
	if !ok {
		return value, "", nil
	}

	password = strings.TrimPrefix(userinfo, ":")
	if decoded, decodeErr := url.QueryUnescape(password); decodeErr == nil {
		password = decoded
	}

	if host == "" {
		return "", "", errors.New("VALKEY_URL host is empty")
	}

	return host, password, nil
}

const loginLimiterCheckScript = `
local max_retry = 0
for i = 1, 3 do
  local count = tonumber(redis.call('GET', KEYS[i]) or '0')
  local limit = tonumber(ARGV[i])
  if count >= limit then
    local ttl = redis.call('TTL', KEYS[i])
    if ttl < 1 then ttl = 1 end
    if ttl > max_retry then max_retry = ttl end
  end
end
return max_retry
`

const loginLimiterFailureScript = `
local window = tonumber(ARGV[1])
local max_count = 0
for i = 1, 3 do
  local count = redis.call('INCR', KEYS[i])
  if count == 1 then redis.call('EXPIRE', KEYS[i], window) end
  if count > max_count then max_count = count end
end
return max_count
`

const loginLimiterSuccessScript = `
redis.call('DEL', KEYS[1])
redis.call('DEL', KEYS[2])
return 1
`
