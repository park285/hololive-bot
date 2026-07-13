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

package orchcmd

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/valkey-io/valkey-go"
)

const (
	expensiveHistoryUserLimit        = 3
	expensiveHistoryRoomLimit        = 12
	expensiveHistoryWindow           = time.Minute
	expensiveHistoryRateLimitMessage = "방송 이력 조회 요청이 너무 많습니다. 잠시 후 다시 시도해 주세요."
	unstableCommandUserID            = "unknown"
)

var (
	errCommandRateLimited          = errors.New("command rate limited")
	errCommandAdmissionUnavailable = errors.New("command admission unavailable")
)

const commandAdmissionKeyPrefix = "bot:command:{history-admission}"

const commandAdmissionScript = `
local now_ms = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local member = ARGV[5]
local ttl_sec = tonumber(ARGV[6])
local cutoff_ms = now_ms - window_ms
local active_min = '(' .. cutoff_ms

for index, key in ipairs(KEYS) do
  local limit = tonumber(ARGV[index + 2])
  local count = redis.call('ZCOUNT', key, active_min, '+inf')
  if count >= limit then
    local retry_after_ms = 0
    local oldest = redis.call('ZRANGEBYSCORE', key, active_min, '+inf', 'WITHSCORES', 'LIMIT', 0, 1)
    if oldest[2] ~= nil then
      retry_after_ms = window_ms - (now_ms - tonumber(oldest[2]))
      if retry_after_ms < 0 then
        retry_after_ms = 0
      end
    end
    return {0, retry_after_ms}
  end
end

for _, key in ipairs(KEYS) do
  redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff_ms)
  redis.call('ZADD', key, now_ms, member)
  redis.call('EXPIRE', key, ttl_sec)
end
return {1, 0}
`

type commandAdmissionLimiter interface {
	Admit(ctx context.Context, checks []commandAdmissionCheck) (commandAdmissionDecision, error)
}

type commandAdmissionDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type commandAdmissionPolicy struct {
	limiter commandAdmissionLimiter
	initErr error
}

type commandAdmissionCheck struct {
	bucket string
	limit  int
}

type atomicCommandAdmissionLimiter struct {
	cacheClient cache.LowLevelCache
	now         func() time.Time
}

func newCommandAdmissionPolicy(cacheClient cache.LowLevelCache) *commandAdmissionPolicy {
	limiter, err := newAtomicCommandAdmissionLimiter(cacheClient)
	return &commandAdmissionPolicy{limiter: limiter, initErr: err}
}

func (p *commandAdmissionPolicy) Admit(ctx context.Context, cmdCtx *domain.CommandContext, commandKey string) error {
	if !isExpensiveHistoryCommand(commandKey) {
		return nil
	}
	if err := p.validateExpensiveHistoryAdmission(cmdCtx); err != nil {
		return err
	}

	checks := []commandAdmissionCheck{
		{bucket: commandAdmissionBucket("history:room", cmdCtx.Room), limit: expensiveHistoryRoomLimit},
		{bucket: commandAdmissionBucket("history:user", cmdCtx.UserID), limit: expensiveHistoryUserLimit},
	}
	decision, err := p.limiter.Admit(ctx, checks)
	if err != nil {
		return fmt.Errorf("%w: evaluate command rate limit: %w", errCommandAdmissionUnavailable, err)
	}
	if !decision.Allowed {
		return fmt.Errorf("%w: retry after %s", errCommandRateLimited, decision.RetryAfter)
	}
	return nil
}

func (p *commandAdmissionPolicy) validateExpensiveHistoryAdmission(cmdCtx *domain.CommandContext) error {
	if p == nil || p.initErr != nil || p.limiter == nil {
		return fmt.Errorf("%w: limiter is not configured", errCommandAdmissionUnavailable)
	}
	if cmdCtx == nil || !isStableAdmissionUserID(cmdCtx.UserID) || strings.TrimSpace(cmdCtx.Room) == "" {
		return fmt.Errorf("%w: stable user and room identities are required", errCommandAdmissionUnavailable)
	}
	return nil
}

func isStableAdmissionUserID(userID string) bool {
	trimmed := strings.TrimSpace(userID)
	return trimmed != "" && !strings.EqualFold(trimmed, unstableCommandUserID)
}

func newAtomicCommandAdmissionLimiter(cacheClient cache.LowLevelCache) (*atomicCommandAdmissionLimiter, error) {
	if cacheClient == nil {
		return nil, errors.New("cache service must not be nil")
	}
	return &atomicCommandAdmissionLimiter{cacheClient: cacheClient, now: time.Now}, nil
}

func (l *atomicCommandAdmissionLimiter) Admit(ctx context.Context, checks []commandAdmissionCheck) (commandAdmissionDecision, error) {
	if err := l.validateChecks(checks); err != nil {
		return commandAdmissionDecision{}, err
	}

	member, err := commandAdmissionMember()
	if err != nil {
		return commandAdmissionDecision{}, fmt.Errorf("generate admission member: %w", err)
	}
	now := l.nowFunc()()
	cmd := l.cacheClient.B().Eval().
		Script(commandAdmissionScript).
		Numkeys(2).
		Key(l.cacheKey(checks[0].bucket), l.cacheKey(checks[1].bucket)).
		Arg(
			strconv.FormatInt(now.UnixMilli(), 10),
			strconv.FormatInt(expensiveHistoryWindow.Milliseconds(), 10),
			strconv.Itoa(checks[0].limit),
			strconv.Itoa(checks[1].limit),
			member,
			strconv.FormatInt(commandAdmissionTTLSeconds(), 10),
		).
		Build()
	results := l.cacheClient.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return commandAdmissionDecision{}, fmt.Errorf("evaluate admission script: unexpected result count: %d", len(results))
	}
	if err := results[0].Error(); err != nil {
		return commandAdmissionDecision{}, fmt.Errorf("evaluate admission script: %w", err)
	}
	return parseCommandAdmissionResult(results[0])
}

func (l *atomicCommandAdmissionLimiter) validateChecks(checks []commandAdmissionCheck) error {
	if l == nil || l.cacheClient == nil {
		return errors.New("cache service must not be nil")
	}
	if len(checks) != 2 {
		return fmt.Errorf("expected two admission checks, got %d", len(checks))
	}
	for _, check := range checks {
		if check.bucket == "" || check.limit <= 0 {
			return errors.New("admission bucket and limit must be configured")
		}
	}
	return nil
}

func (l *atomicCommandAdmissionLimiter) nowFunc() func() time.Time {
	if l.now == nil {
		return time.Now
	}
	return l.now
}

func (l *atomicCommandAdmissionLimiter) cacheKey(bucket string) string {
	return commandAdmissionKeyPrefix + ":" + bucket
}

func commandAdmissionMember() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func commandAdmissionTTLSeconds() int64 {
	return int64((expensiveHistoryWindow + time.Second - 1) / time.Second)
}

func parseCommandAdmissionResult(result valkey.ValkeyResult) (commandAdmissionDecision, error) {
	values, err := result.ToArray()
	if err != nil {
		return commandAdmissionDecision{}, fmt.Errorf("parse admission script result: %w", err)
	}
	if len(values) != 2 {
		return commandAdmissionDecision{}, fmt.Errorf("invalid admission script result length: %d", len(values))
	}
	allowed, err := values[0].AsInt64()
	if err != nil {
		return commandAdmissionDecision{}, fmt.Errorf("parse admission result: %w", err)
	}
	retryAfterMS, err := values[1].AsInt64()
	if err != nil {
		return commandAdmissionDecision{}, fmt.Errorf("parse admission retry after: %w", err)
	}
	if retryAfterMS < 0 {
		return commandAdmissionDecision{}, fmt.Errorf("invalid admission retry after: %d", retryAfterMS)
	}
	return commandAdmissionDecision{Allowed: allowed == 1, RetryAfter: time.Duration(retryAfterMS) * time.Millisecond}, nil
}

func isExpensiveHistoryCommand(commandKey string) bool {
	switch commandKey {
	case "broadcast_history", "broadcast_thumbnail":
		return true
	default:
		return false
	}
}

func commandAdmissionBucket(scope, identity string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(identity)))
	return scope + ":" + hex.EncodeToString(digest[:])
}
