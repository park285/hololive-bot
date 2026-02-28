package valkeyx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"
)

// LockConfig는 분산 락의 동작을 설정합니다.
type LockConfig struct {
	// TTL: 락 만료 시간
	TTL time.Duration

	// RetryInterval: 락 획득 실패 시 재시도 간격 (첫 시도)
	RetryInterval time.Duration

	// MaxRetries: 최대 재시도 횟수 (0 = context timeout까지 무한 재시도)
	MaxRetries int

	// KeyPrefix: 락 키 접두사 (예: "20q:lock:")
	KeyPrefix string

	// EnableHolder: 락 보유자 정보 추적 활성화
	EnableHolder bool

	// HolderName: 락 보유자 이름 (EnableHolder=true 시 사용)
	HolderName string

	// EnableRenewal: 자동 TTL 갱신 활성화 (watchdog)
	EnableRenewal bool

	// RenewalInterval: 갱신 주기 (기본값: TTL/3, 최소 1초)
	RenewalInterval time.Duration
}

// DefaultLockConfig는 기본 락 설정을 반환합니다.
func DefaultLockConfig() LockConfig {
	return LockConfig{
		TTL:           10 * time.Second,
		RetryInterval: 50 * time.Millisecond,
		MaxRetries:    0, // 무한 재시도
		KeyPrefix:     "lock:",
		EnableHolder:  false,
		EnableRenewal: false,
	}
}

// Lock은 분산 락 인터페이스입니다.
type Lock interface {
	// Unlock: 락을 해제합니다.
	Unlock(ctx context.Context) error

	// Extend: 락 TTL을 연장합니다. 성공 여부와 에러를 반환합니다.
	Extend(ctx context.Context, ttl time.Duration) (bool, error)

	// Token: 락 토큰을 반환합니다.
	Token() string
}

type distributedLock struct {
	client        valkey.Client
	key           string
	holderKey     string
	token         string
	config        LockConfig
	cancelRenewal context.CancelFunc
}

// Token은 락의 고유 토큰을 반환합니다.
func (l *distributedLock) Token() string {
	return l.token
}

// Unlock은 락을 해제합니다.
func (l *distributedLock) Unlock(ctx context.Context) error {
	if l.cancelRenewal != nil {
		l.cancelRenewal()
	}

	if l.config.EnableHolder {
		return l.releaseWithHolder(ctx)
	}
	return l.release(ctx)
}

// Extend는 락의 TTL을 연장합니다.
func (l *distributedLock) Extend(ctx context.Context, ttl time.Duration) (bool, error) {
	return l.renew(ctx, ttl)
}

// TryAcquire는 블로킹 없이 락 획득을 시도합니다.
// 성공 시 Lock 인터페이스와 true를 반환하고, 실패 시 nil과 false를 반환합니다.
func TryAcquire(ctx context.Context, client valkey.Client, key string, cfg LockConfig) (Lock, bool, error) {
	cfg.MaxRetries = 1
	cfg.RetryInterval = 0
	return Acquire(ctx, client, key, cfg)
}

// Acquire는 분산 락을 획득합니다.
// 재시도 설정에 따라 context timeout까지 또는 MaxRetries까지 재시도합니다.
func Acquire(ctx context.Context, client valkey.Client, key string, cfg LockConfig) (Lock, bool, error) {
	if client == nil {
		return nil, false, errors.New("valkey client is nil")
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return nil, false, fmt.Errorf("lock key is empty")
	}

	if cfg.TTL <= 0 {
		cfg.TTL = 10 * time.Second
	}
	if cfg.RetryInterval <= 0 {
		cfg.RetryInterval = 50 * time.Millisecond
	}

	token, err := generateToken()
	if err != nil {
		return nil, false, fmt.Errorf("generate lock token: %w", err)
	}

	lockKey := cfg.KeyPrefix + "{" + key + "}"
	holderKey := ""
	if cfg.EnableHolder {
		holderKey = cfg.KeyPrefix + "holder:{" + key + "}"
	}

	lock := &distributedLock{
		client:    client,
		key:       lockKey,
		holderKey: holderKey,
		token:     token,
		config:    cfg,
	}

	acquired, err := lock.acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	if !acquired {
		return nil, false, nil
	}

	// 자동 갱신 활성화
	if cfg.EnableRenewal {
		lock.cancelRenewal = lock.startRenewalWatchdog(ctx)
	}

	return lock, true, nil
}

// acquire는 락 획득을 시도합니다 (재시도 로직 포함).
func (l *distributedLock) acquire(ctx context.Context) (bool, error) {
	delay := l.config.RetryInterval
	maxDelay := 500 * time.Millisecond
	delayMultiply := 2

	// TTL을 밀리초로 변환 (최소 1ms)
	ttlMillis := max(l.config.TTL.Milliseconds(), 1)

	attempt := 0
	for {
		// SET NX PX 시도 (PX는 밀리초 단위로 더 정밀)
		cmd := l.client.B().Set().Key(l.key).Value(l.token).Nx().Px(time.Duration(ttlMillis) * time.Millisecond).Build()
		result, err := l.client.Do(ctx, cmd).ToString()

		// SET NX 성공 시 "OK" 반환, 실패(키 존재) 시 nil error with empty result
		if err == nil && result == "OK" {
			// 획득 성공
			if l.config.EnableHolder {
				holderValue := l.buildHolderValue()
				holderCmd := l.client.B().Set().Key(l.holderKey).Value(holderValue).Px(time.Duration(ttlMillis) * time.Millisecond).Build()
				if holderErr := l.client.Do(ctx, holderCmd).Error(); holderErr != nil {
					// holder 설정 실패 시 락 해제
					if releaseErr := l.release(ctx); releaseErr != nil {
						// 클린업 실패 로깅 (메인 에러는 holderErr)
						slog.WarnContext(ctx, "lock release during cleanup failed",
							slog.String("key", l.key),
							slog.Any("release_error", releaseErr),
							slog.Any("original_error", holderErr))
					}
					return false, fmt.Errorf("set lock holder: %w", holderErr)
				}
			}
			return true, nil
		}

		if !isNil(err) && err != nil {
			if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return false, nil
			}
			return false, fmt.Errorf("lock acquire: %w", err)
		}

		// 재시도 로직
		attempt++
		if l.config.MaxRetries > 0 && attempt >= l.config.MaxRetries {
			return false, nil
		}

		// Context 취소 확인
		if ctx.Err() != nil {
			return false, nil
		}

		// Exponential backoff
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
				delay = min(delay*time.Duration(delayMultiply), maxDelay)
			case <-ctx.Done():
				timer.Stop()
				return false, nil
			}
		}
	}
}

// release는 락을 해제합니다 (토큰 검증 포함).
func (l *distributedLock) release(ctx context.Context) error {
	// Lua 스크립트: GET + DEL 원자적 실행
	script := `
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("DEL", KEYS[1])
else
    return 0
end
`
	resp := l.client.Do(ctx, l.client.B().Eval().Script(script).Numkeys(1).Key(l.key).Arg(l.token).Build())
	return resp.Error()
}

// releaseWithHolder는 락과 holder 정보를 함께 해제합니다.
func (l *distributedLock) releaseWithHolder(ctx context.Context) error {
	// Lua 스크립트: lock + holder 원자적 해제
	script := `
local lockKey = KEYS[1]
local holderKey = KEYS[2]
local token = ARGV[1]

local deleted = 0
if redis.call("GET", lockKey) == token then
    deleted = redis.call("DEL", lockKey)
end

local holderVal = redis.call("GET", holderKey)
if holderVal then
    local delimPos = string.find(holderVal, "|", 1, true)
    if delimPos then
        local holderToken = string.sub(holderVal, 1, delimPos - 1)
        if holderToken == token then
            redis.call("DEL", holderKey)
        end
    end
end

return deleted
`
	resp := l.client.Do(ctx, l.client.B().Eval().Script(script).Numkeys(2).Key(l.key).Key(l.holderKey).Arg(l.token).Build())
	return resp.Error()
}

// renew는 락의 TTL을 갱신합니다.
func (l *distributedLock) renew(ctx context.Context, ttl time.Duration) (bool, error) {
	// Lua 스크립트: GET + PEXPIRE 원자적 실행
	script := `
if redis.call("GET", KEYS[1]) == ARGV[1] then
    return redis.call("PEXPIRE", KEYS[1], ARGV[2])
else
    return 0
end
`
	ttlMillis := int64(ttl / time.Millisecond)
	resp := l.client.Do(ctx, l.client.B().Eval().Script(script).Numkeys(1).Key(l.key).Arg(l.token).Arg(fmt.Sprintf("%d", ttlMillis)).Build())

	if err := resp.Error(); err != nil {
		return false, err
	}

	result, err := resp.AsInt64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

// startRenewalWatchdog는 백그라운드에서 주기적으로 락을 갱신합니다.
func (l *distributedLock) startRenewalWatchdog(ctx context.Context) context.CancelFunc {
	interval := l.config.RenewalInterval
	if interval <= 0 {
		interval = l.config.TTL / 3
	}
	if interval < time.Second {
		interval = time.Second
	}

	renewCtx, cancel := context.WithCancel(ctx)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-renewCtx.Done():
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				callCtx, callCancel := context.WithTimeout(renewCtx, 5*time.Second)
				renewed, err := l.renew(callCtx, l.config.TTL)
				callCancel()

				if err != nil || !renewed {
					// 갱신 실패 시 중단
					return
				}
			}
		}
	}()

	return cancel
}

// buildHolderValue는 holder 값을 생성합니다 (token|name 형식).
func (l *distributedLock) buildHolderValue() string {
	name := "다른 사용자"
	if l.config.HolderName != "" {
		name = strings.TrimSpace(l.config.HolderName)
	}
	return l.token + "|" + name
}

// generateToken은 락 식별용 랜덤 토큰을 생성합니다.
func generateToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("rand read: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// isNil은 발생한 에러가 Valkey nil(키가 없음) 에러인지 확인합니다.
func isNil(err error) bool {
	if valkey.IsValkeyNil(err) {
		return true
	}
	// fmt.Errorf("%w", err)로 래핑된 경우 언래핑하여 체크
	unwrapped := err
	for unwrapped != nil {
		if valkey.IsValkeyNil(unwrapped) {
			return true
		}
		unwrapped = errors.Unwrap(unwrapped)
	}
	return false
}
