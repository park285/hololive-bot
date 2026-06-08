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

var (
	globalBudgetReserveLua = valkey.NewLuaScript(globalBudgetReserveScript)
	globalBudgetReleaseLua = valkey.NewLuaScript(globalBudgetReleaseScript)
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
local reservationMember = ARGV[1]
local className = ARGV[2]
local units = ARGV[3]
local nowMS = tonumber(ARGV[4])
local ttlMS = tonumber(ARGV[5])
local sourceMax = tonumber(ARGV[6])
local classMax = tonumber(ARGV[7])
local deniedRetryAfterMS = tonumber(ARGV[8])
local reservationPrefix = ARGV[9]
local budgetPrefix = ARGV[10]
local cleanupLimit = tonumber(ARGV[11]) or 128
if cleanupLimit <= 0 then
  cleanupLimit = 128
end

local cleanupGraceMS = deniedRetryAfterMS
if cleanupGraceMS < 60000 then
  cleanupGraceMS = 60000
end
local reservationTTLMS = ttlMS + cleanupGraceMS
if reservationTTLMS <= ttlMS then
  reservationTTLMS = ttlMS
end

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

local function pexpireAtLeast(key, currentTTL, ttl)
  if ttl <= 0 then
    return
  end
  if currentTTL == -1 then
    return
  end
  if currentTTL == -2 or currentTTL < ttl then
    redis.call('PEXPIRE', key, ttl)
  end
end

local function classForExpiredMember(member)
  local sep = string.find(member, '|', 1, true)
  if sep ~= nil then
    return string.sub(member, 1, sep - 1)
  end
  return redis.call('HGET', reservationPrefix .. member, 'class')
end

local expiredMembers = redis.call('ZRANGEBYSCORE', reservationsKey, '-inf', nowMS, 'LIMIT', 0, cleanupLimit)
for _, expiredMember in ipairs(expiredMembers) do
  local expiredClass = classForExpiredMember(expiredMember)
  if expiredClass then
    decrementInflight(classKeyFor(expiredClass))
    decrementInflight(globalKey)
    redis.call('DEL', reservationPrefix .. expiredMember)
  end
  redis.call('ZREM', reservationsKey, expiredMember)
end

local cleanupIncomplete = false
local remainingExpired = redis.call('ZRANGEBYSCORE', reservationsKey, '-inf', nowMS, 'LIMIT', 0, 1)
if #remainingExpired > 0 then
  cleanupIncomplete = true
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
  if cleanupIncomplete then
    return {0, deniedRetryAfterMS, 'budget_cleanup_incomplete'}
  end
  return {0, deniedRetryAfterMS, 'budget_exhausted'}
end

local classCurrent = currentValue(currentClassKey)
if classMax > 0 and classCurrent >= classMax then
  if cleanupIncomplete then
    return {0, deniedRetryAfterMS, 'budget_cleanup_incomplete'}
  end
  return {0, deniedRetryAfterMS, 'budget_exhausted'}
end

local classTTL = redis.call('PTTL', currentClassKey)
local globalTTL = redis.call('PTTL', globalKey)
local reservationsTTL = redis.call('PTTL', reservationsKey)
local reservationTTL = redis.call('PTTL', reservationKey)
redis.call('INCR', currentClassKey)
redis.call('INCR', globalKey)
redis.call('HSET', reservationKey, 'class', className, 'units', units, 'member', reservationMember)
redis.call('ZADD', reservationsKey, nowMS + ttlMS, reservationMember)
pexpireAtLeast(currentClassKey, classTTL, reservationTTLMS)
pexpireAtLeast(globalKey, globalTTL, reservationTTLMS)
pexpireAtLeast(reservationsKey, reservationsTTL, reservationTTLMS)
pexpireAtLeast(reservationKey, reservationTTL, reservationTTLMS)
return {1, 0, ''}
`

const globalBudgetReleaseScript = `
local globalKey = KEYS[1]
local reservationsKey = KEYS[2]
local reservationKey = KEYS[3]
local primaryClassKey = KEYS[4]
local backfillClassKey = KEYS[5]
local fallbackClassKey = KEYS[6]
local reservationMember = ARGV[1]
local budgetPrefix = ARGV[2]
local className = ARGV[3]

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

local removed = redis.call('ZREM', reservationsKey, reservationMember)
local hashExists = redis.call('EXISTS', reservationKey)
if removed == 1 or hashExists == 1 then
  decrementInflight(classKeyFor(className))
  decrementInflight(globalKey)
  redis.call('DEL', reservationKey)
  return 1
end
redis.call('DEL', reservationKey)
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

func evalGlobalBudgetArray(
	ctx context.Context,
	cacheClient cache.Client,
	script *valkey.Lua,
	keys []string,
	args []string,
	action string,
) ([]valkey.ValkeyMessage, error) {
	if cacheClient == nil || cacheClient.GetClient() == nil {
		return nil, fmt.Errorf("%s: cache client must not be nil", action)
	}
	if script == nil {
		return nil, fmt.Errorf("%s: lua script must not be nil", action)
	}
	result := script.Exec(ctx, cacheClient.GetClient(), keys, args)
	if result.Error() != nil {
		return nil, fmt.Errorf("%s: %w", action, result.Error())
	}
	values, err := result.ToArray()
	if err != nil {
		return nil, fmt.Errorf("%s: parse result: %w", action, err)
	}
	return values, nil
}

func evalGlobalBudgetInt(
	ctx context.Context,
	cacheClient cache.Client,
	script *valkey.Lua,
	keys []string,
	args []string,
	action string,
) error {
	if cacheClient == nil || cacheClient.GetClient() == nil {
		return fmt.Errorf("%s: cache client must not be nil", action)
	}
	if script == nil {
		return fmt.Errorf("%s: lua script must not be nil", action)
	}
	result := script.Exec(ctx, cacheClient.GetClient(), keys, args)
	if result.Error() != nil {
		return fmt.Errorf("%s: %w", action, result.Error())
	}
	if _, err := result.AsInt64(); err != nil {
		return fmt.Errorf("%s: parse result: %w", action, err)
	}
	return nil
}
