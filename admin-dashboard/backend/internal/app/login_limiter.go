package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"
)

const (
	loginLimitWindow       = 15 * time.Minute
	loginIPFailureLimit    = 10
	loginAccountFailureLimit = 30
	loginGlobalFailureLimit  = 200
)

type distributedLoginLimiter struct {
	client valkey.Client
}

func newDistributedLoginLimiter(ctx context.Context, valkeyURL string) (*distributedLoginLimiter, error) {
	addr, password, err := parseLoginLimiterValkeyAddress(valkeyURL)
	if err != nil {
		return nil, err
	}
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{addr},
		Password:          password,
		PipelineMultiplex: 4,
		BlockingPoolSize:  32,
		Dialer:            net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second},
		ConnWriteTimeout:  3 * time.Second,
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
	return &distributedLoginLimiter{client: client}, nil
}

func (l *distributedLoginLimiter) Close() {
	if l != nil && l.client != nil {
		l.client.Close()
	}
}

// Check returns the longest retry delay among the IP, account and global
// failure buckets. A zero duration means the request may proceed.
func (l *distributedLoginLimiter) Check(ctx context.Context, ip, account string) (time.Duration, error) {
	if l == nil || l.client == nil {
		return 0, nil
	}
	keys := loginLimiterKeys(ip, account)
	result, err := l.evalInt(ctx, loginLimiterCheckScript, keys, []string{
		fmt.Sprint(loginIPFailureLimit),
		fmt.Sprint(loginAccountFailureLimit),
		fmt.Sprint(loginGlobalFailureLimit),
	})
	if err != nil {
		return 0, err
	}
	if result <= 0 {
		return 0, nil
	}
	return time.Duration(result) * time.Second, nil
}

func (l *distributedLoginLimiter) RecordFailure(ctx context.Context, ip, account string) (int, error) {
	if l == nil || l.client == nil {
		return 0, nil
	}
	result, err := l.evalInt(ctx, loginLimiterFailureScript, loginLimiterKeys(ip, account), []string{
		fmt.Sprint(int(loginLimitWindow.Seconds())),
	})
	if err != nil {
		return 0, err
	}
	return int(result), nil
}

func (l *distributedLoginLimiter) RecordSuccess(ctx context.Context, ip, account string) error {
	if l == nil || l.client == nil {
		return nil
	}
	keys := loginLimiterKeys(ip, account)
	_, err := l.evalInt(ctx, loginLimiterSuccessScript, keys[:2], nil)
	return err
}

func (l *distributedLoginLimiter) evalInt(ctx context.Context, script string, keys, args []string) (int64, error) {
	cmd := l.client.B().Eval().Script(script).Numkeys(int64(len(keys))).Key(keys...).Arg(args...).Build()
	resp := l.client.Do(ctx, cmd)
	if err := resp.Error(); err != nil {
		return 0, err
	}
	return resp.AsInt64()
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
		return "", "", fmt.Errorf("VALKEY_URL host is empty")
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
