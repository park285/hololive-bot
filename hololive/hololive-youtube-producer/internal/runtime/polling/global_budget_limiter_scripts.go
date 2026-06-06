package polling

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/valkey-io/valkey-go"
)

const (
	globalBudgetReserveCodeDenied  int64 = 0
	globalBudgetReserveCodeAllowed int64 = 1
)

const globalBudgetReserveScript = `
local currentClassKey = KEYS[1]
local globalKey = KEYS[2]
local reservationsKey = KEYS[3]
local reservationKey = KEYS[4]
local cooldownKey = KEYS[5]
local primaryClassKey = KEYS[6]
local backfillClassKey = KEYS[7]
local fallbackClassKey = KEYS[8]
local ownerToken = ARGV[1]
local className = ARGV[2]
local units = ARGV[3]
local nowMS = tonumber(ARGV[4])
local ttlMS = tonumber(ARGV[5])
local sourceMax = tonumber(ARGV[6])
local classMax = tonumber(ARGV[7])
local deniedRetryAfterMS = tonumber(ARGV[8])
local reservationPrefix = ARGV[9]
local budgetPrefix = ARGV[10]

local function classKeyFor(name)
  if name == 'primary' then
    return primaryClassKey
  end
  if name == 'backfill' then
    return backfillClassKey
  end
  if name == 'fallback' then
    return fallbackClassKey
  end
  return budgetPrefix .. name .. ':inflight'
end

local function currentValue(key)
  return tonumber(redis.call('GET', key) or '0')
end

local function decrementInflight(key)
  local value = currentValue(key)
  if value <= 0 then
    redis.call('DEL', key)
    return 0
  end
  local nextValue = value - 1
  if nextValue <= 0 then
    redis.call('DEL', key)
    return 0
  end
  redis.call('DECR', key)
  return nextValue
end

local expiredTokens = redis.call('ZRANGEBYSCORE', reservationsKey, '-inf', nowMS)
for _, expiredToken in ipairs(expiredTokens) do
  local expiredReservationKey = reservationPrefix .. expiredToken
  if redis.call('EXISTS', expiredReservationKey) == 1 then
    local expiredClass = redis.call('HGET', expiredReservationKey, 'class')
    if expiredClass then
      decrementInflight(classKeyFor(expiredClass))
    end
    decrementInflight(globalKey)
    redis.call('DEL', expiredReservationKey)
  end
  redis.call('ZREM', reservationsKey, expiredToken)
end

if redis.call('EXISTS', cooldownKey) == 1 then
  local ttl = redis.call('PTTL', cooldownKey)
  if ttl < 0 then
    ttl = deniedRetryAfterMS
  end
  return {0, ttl, 'source_cooldown'}
end

local globalCurrent = currentValue(globalKey)
if sourceMax > 0 and globalCurrent >= sourceMax then
  return {0, deniedRetryAfterMS, 'budget_exhausted'}
end

local classCurrent = currentValue(currentClassKey)
if classMax > 0 and classCurrent >= classMax then
  return {0, deniedRetryAfterMS, 'budget_exhausted'}
end

redis.call('INCR', currentClassKey)
redis.call('INCR', globalKey)
redis.call('HSET', reservationKey, 'class', className, 'units', units)
redis.call('ZADD', reservationsKey, nowMS + ttlMS, ownerToken)
return {1, 0, ''}
`

const globalBudgetReleaseScript = `
local globalKey = KEYS[1]
local reservationsKey = KEYS[2]
local reservationKey = KEYS[3]
local primaryClassKey = KEYS[4]
local backfillClassKey = KEYS[5]
local fallbackClassKey = KEYS[6]
local ownerToken = ARGV[1]
local budgetPrefix = ARGV[2]

local function classKeyFor(name)
  if name == 'primary' then
    return primaryClassKey
  end
  if name == 'backfill' then
    return backfillClassKey
  end
  if name == 'fallback' then
    return fallbackClassKey
  end
  return budgetPrefix .. name .. ':inflight'
end

local function currentValue(key)
  return tonumber(redis.call('GET', key) or '0')
end

local function decrementInflight(key)
  local value = currentValue(key)
  if value <= 0 then
    redis.call('DEL', key)
    return 0
  end
  local nextValue = value - 1
  if nextValue <= 0 then
    redis.call('DEL', key)
    return 0
  end
  redis.call('DECR', key)
  return nextValue
end

if redis.call('EXISTS', reservationKey) == 1 then
  local className = redis.call('HGET', reservationKey, 'class')
  if className then
    decrementInflight(classKeyFor(className))
  end
  decrementInflight(globalKey)
  redis.call('DEL', reservationKey)
  redis.call('ZREM', reservationsKey, ownerToken)
  return 1
end
redis.call('ZREM', reservationsKey, ownerToken)
return 0
`

func parseGlobalBudgetReserveResult(values []valkey.ValkeyMessage) (poller.BudgetDecision, error) {
	code, retryAfterMS, reason, err := parseGlobalBudgetReserveValues(values)
	if err != nil {
		return poller.BudgetDecision{}, err
	}
	switch code {
	case globalBudgetReserveCodeAllowed:
		return poller.BudgetDecision{Allowed: true}, nil
	case globalBudgetReserveCodeDenied:
		return poller.BudgetDecision{
			Allowed:    false,
			RetryAfter: millisDuration(retryAfterMS),
			Reason:     reason,
		}, nil
	default:
		return poller.BudgetDecision{}, fmt.Errorf("reserve global budget: unknown result code: %d", code)
	}
}

func parseGlobalBudgetReserveValues(values []valkey.ValkeyMessage) (int64, int64, string, error) {
	if len(values) != 3 {
		return 0, 0, "", fmt.Errorf("reserve global budget: unexpected result length: %d", len(values))
	}
	code, err := values[0].AsInt64()
	if err != nil {
		return 0, 0, "", fmt.Errorf("reserve global budget: parse result code: %w", err)
	}
	retryAfterMS, err := values[1].AsInt64()
	if err != nil {
		return 0, 0, "", fmt.Errorf("reserve global budget: parse retry after: %w", err)
	}
	reason, err := values[2].ToString()
	if err != nil {
		return 0, 0, "", fmt.Errorf("reserve global budget: parse reason: %w", err)
	}
	return code, retryAfterMS, reason, nil
}

func evalGlobalBudgetArray(ctx context.Context, cacheClient cache.Client, cmd valkey.Completed, action string) ([]valkey.ValkeyMessage, error) {
	results := cacheClient.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return nil, fmt.Errorf("%s: unexpected result count: %d", action, len(results))
	}
	if results[0].Error() != nil {
		return nil, fmt.Errorf("%s: %w", action, results[0].Error())
	}
	values, err := results[0].ToArray()
	if err != nil {
		return nil, fmt.Errorf("%s: parse result: %w", action, err)
	}
	return values, nil
}

func evalGlobalBudgetInt(ctx context.Context, cacheClient cache.Client, cmd valkey.Completed, action string) error {
	results := cacheClient.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return fmt.Errorf("%s: unexpected result count: %d", action, len(results))
	}
	if results[0].Error() != nil {
		return fmt.Errorf("%s: %w", action, results[0].Error())
	}
	if _, err := results[0].AsInt64(); err != nil {
		return fmt.Errorf("%s: parse result: %w", action, err)
	}
	return nil
}
