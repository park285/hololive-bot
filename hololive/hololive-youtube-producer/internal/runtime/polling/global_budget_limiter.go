package polling

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/valkey-io/valkey-go"
)

type GlobalBudgetLimiterConfig struct {
	Namespace          string
	InstanceID         string
	SourceMaxInflight  map[poller.BudgetSource]int
	ClassMaxInflight   map[poller.BudgetBurstClass]int
	DeniedRetryAfter   time.Duration
	WindowCheckEnabled bool
}

type globalBudgetLimiter struct {
	cacheClient        cache.Client
	namespace          string
	instanceID         string
	sourceMaxInflight  map[poller.BudgetSource]int
	classMaxInflight   map[poller.BudgetBurstClass]int
	deniedRetryAfter   time.Duration
	windowCheckEnabled bool
}

type globalBudgetReservation struct {
	cacheClient cache.Client
	namespace   string
	ownerToken  string
	sources     []poller.BudgetSource
	state       atomic.Uint32
}

const (
	defaultGlobalBudgetDeniedRetryAfter = 5 * time.Second

	globalBudgetReserveCodeDenied  int64 = 0
	globalBudgetReserveCodeAllowed int64 = 1

	globalBudgetReservationActive     uint32 = 0
	globalBudgetReservationInProgress uint32 = 1
	globalBudgetReservationDone       uint32 = 2
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
    return 0
  end
  local nextValue = value - 1
  redis.call('SET', key, nextValue)
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
    return 0
  end
  local nextValue = value - 1
  redis.call('SET', key, nextValue)
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

func NewGlobalBudgetLimiter(cacheClient cache.Client, cfg GlobalBudgetLimiterConfig) (poller.GlobalBudgetLimiter, error) {
	if cacheClient == nil {
		return nil, fmt.Errorf("new global budget limiter: cache service must not be nil")
	}
	namespace := normalizeGlobalBudgetNamespace(cfg.Namespace)
	if namespace == "" {
		return nil, fmt.Errorf("new global budget limiter: namespace must not be empty")
	}
	deniedRetryAfter := cfg.DeniedRetryAfter
	if deniedRetryAfter <= 0 {
		deniedRetryAfter = defaultGlobalBudgetDeniedRetryAfter
	}
	return &globalBudgetLimiter{
		cacheClient:        cacheClient,
		namespace:          namespace,
		instanceID:         normalizeGlobalBudgetInstanceID(cfg.InstanceID),
		sourceMaxInflight:  copySourceMaxInflight(cfg.SourceMaxInflight),
		classMaxInflight:   copyClassMaxInflight(cfg.ClassMaxInflight),
		deniedRetryAfter:   deniedRetryAfter,
		windowCheckEnabled: cfg.WindowCheckEnabled,
	}, nil
}

func (l *globalBudgetLimiter) TryReserve(
	ctx context.Context,
	job poller.BudgetJob,
	profile poller.BudgetProfile,
	ttl time.Duration,
) (poller.BudgetReservation, poller.BudgetDecision, error) {
	if len(profile.SourceUnits) == 0 {
		return nil, poller.BudgetDecision{Allowed: true}, nil
	}
	if ttl <= 0 {
		return nil, poller.BudgetDecision{}, fmt.Errorf("try reserve global budget: ttl must be positive")
	}

	ownerToken, err := l.newOwnerToken(job)
	if err != nil {
		return nil, poller.BudgetDecision{}, fmt.Errorf("try reserve global budget: owner token: %w", err)
	}

	sources := sortedBudgetSources(profile.SourceUnits)
	acquired := make([]poller.BudgetSource, 0, len(sources))
	nowMS := time.Now().UnixMilli()
	ttlMS := durationMillis(ttl)
	for _, source := range sources {
		decision, err := l.reserveSource(ctx, source, profile, ownerToken, nowMS, ttlMS)
		if err != nil {
			_ = l.releaseSources(ctx, ownerToken, acquired)
			return nil, poller.BudgetDecision{}, fmt.Errorf("try reserve global budget: source %s: %w", source, err)
		}
		if !decision.Allowed {
			if rollbackErr := l.releaseSources(ctx, ownerToken, acquired); rollbackErr != nil {
				return nil, poller.BudgetDecision{}, fmt.Errorf("try reserve global budget: rollback source %s: %w", source, rollbackErr)
			}
			return nil, decision, nil
		}
		acquired = append(acquired, source)
	}

	return &globalBudgetReservation{
		cacheClient: l.cacheClient,
		namespace:   l.namespace,
		ownerToken:  ownerToken,
		sources:     acquired,
	}, poller.BudgetDecision{Allowed: true}, nil
}

func (l *globalBudgetLimiter) reserveSource(
	ctx context.Context,
	source poller.BudgetSource,
	profile poller.BudgetProfile,
	ownerToken string,
	nowMS int64,
	ttlMS int64,
) (poller.BudgetDecision, error) {
	keys := l.keys(source, profile.BurstClass, ownerToken)
	units := profile.SourceUnits[source]
	cmd := l.cacheClient.B().
		Eval().
		Script(globalBudgetReserveScript).
		Numkeys(8).
		Key(
			keys.ClassInflight,
			keys.GlobalInflight,
			keys.Reservations,
			keys.Reservation,
			keys.SourceCooldown,
			keys.PrimaryInflight,
			keys.BackfillInflight,
			keys.FallbackInflight,
		).
		Arg(
			ownerToken,
			string(profile.BurstClass),
			strconv.FormatFloat(units, 'f', -1, 64),
			strconv.FormatInt(nowMS, 10),
			strconv.FormatInt(ttlMS, 10),
			strconv.Itoa(l.sourceMaxInflight[source]),
			strconv.Itoa(l.classMaxInflight[profile.BurstClass]),
			strconv.FormatInt(durationMillis(l.deniedRetryAfter), 10),
			keys.ReservationPrefix,
			keys.BudgetPrefix,
		).
		Build()
	values, err := evalGlobalBudgetArray(ctx, l.cacheClient, cmd, "reserve global budget")
	if err != nil {
		return poller.BudgetDecision{}, err
	}
	return parseGlobalBudgetReserveResult(values)
}

func (r *globalBudgetReservation) Commit(ctx context.Context) error {
	return r.terminal(ctx, "commit global budget")
}

func (r *globalBudgetReservation) Release(ctx context.Context) error {
	return r.terminal(ctx, "release global budget")
}

func (r *globalBudgetReservation) terminal(ctx context.Context, action string) error {
	if r == nil || r.cacheClient == nil {
		return fmt.Errorf("%s: reservation must not be nil", action)
	}
	if !r.state.CompareAndSwap(globalBudgetReservationActive, globalBudgetReservationInProgress) {
		return nil
	}
	err := r.releaseAll(ctx, action)
	if err != nil {
		r.state.Store(globalBudgetReservationActive)
		return err
	}
	r.state.Store(globalBudgetReservationDone)
	return nil
}

func (r *globalBudgetReservation) releaseAll(ctx context.Context, action string) error {
	var firstErr error
	for _, source := range r.sources {
		keys := buildGlobalBudgetKeys(r.namespace, source, poller.BudgetBurstPrimary, r.ownerToken)
		cmd := r.cacheClient.B().
			Eval().
			Script(globalBudgetReleaseScript).
			Numkeys(6).
			Key(
				keys.GlobalInflight,
				keys.Reservations,
				keys.Reservation,
				keys.PrimaryInflight,
				keys.BackfillInflight,
				keys.FallbackInflight,
			).
			Arg(r.ownerToken, keys.BudgetPrefix).
			Build()
		if err := evalGlobalBudgetInt(ctx, r.cacheClient, cmd, action); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: source %s: %w", action, source, err)
		}
	}
	return firstErr
}

func (l *globalBudgetLimiter) releaseSources(ctx context.Context, ownerToken string, sources []poller.BudgetSource) error {
	reservation := globalBudgetReservation{
		cacheClient: l.cacheClient,
		namespace:   l.namespace,
		ownerToken:  ownerToken,
		sources:     sources,
	}
	return reservation.releaseAll(ctx, "rollback global budget")
}

func parseGlobalBudgetReserveResult(values []valkey.ValkeyMessage) (poller.BudgetDecision, error) {
	if len(values) != 3 {
		return poller.BudgetDecision{}, fmt.Errorf("reserve global budget: unexpected result length: %d", len(values))
	}
	code, err := values[0].AsInt64()
	if err != nil {
		return poller.BudgetDecision{}, fmt.Errorf("reserve global budget: parse result code: %w", err)
	}
	retryAfterMS, err := values[1].AsInt64()
	if err != nil {
		return poller.BudgetDecision{}, fmt.Errorf("reserve global budget: parse retry after: %w", err)
	}
	reason, err := values[2].ToString()
	if err != nil {
		return poller.BudgetDecision{}, fmt.Errorf("reserve global budget: parse reason: %w", err)
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

type globalBudgetKeys struct {
	BudgetPrefix      string
	ReservationPrefix string
	ClassInflight     string
	GlobalInflight    string
	Reservations      string
	Reservation       string
	SourceCooldown    string
	PrimaryInflight   string
	BackfillInflight  string
	FallbackInflight  string
}

func (l *globalBudgetLimiter) keys(source poller.BudgetSource, class poller.BudgetBurstClass, ownerToken string) globalBudgetKeys {
	return buildGlobalBudgetKeys(l.namespace, source, class, ownerToken)
}

func buildGlobalBudgetKeys(namespace string, source poller.BudgetSource, class poller.BudgetBurstClass, ownerToken string) globalBudgetKeys {
	sourceTag := string(source)
	budgetPrefix := fmt.Sprintf("hololive:%s:youtube-producer:budget:{%s}:", namespace, sourceTag)
	reservationPrefix := budgetPrefix + "reservation:"
	return globalBudgetKeys{
		BudgetPrefix:      budgetPrefix,
		ReservationPrefix: reservationPrefix,
		ClassInflight:     budgetPrefix + string(class) + ":inflight",
		GlobalInflight:    budgetPrefix + "global:inflight",
		Reservations:      budgetPrefix + "reservations",
		Reservation:       reservationPrefix + ownerToken,
		SourceCooldown:    fmt.Sprintf("hololive:%s:youtube-producer:source-cooldown:{%s}", namespace, sourceTag),
		PrimaryInflight:   budgetPrefix + string(poller.BudgetBurstPrimary) + ":inflight",
		BackfillInflight:  budgetPrefix + string(poller.BudgetBurstBackfill) + ":inflight",
		FallbackInflight:  budgetPrefix + string(poller.BudgetBurstFallback) + ":inflight",
	}
}

func sortedBudgetSources(sourceUnits map[poller.BudgetSource]float64) []poller.BudgetSource {
	sources := make([]poller.BudgetSource, 0, len(sourceUnits))
	for source := range sourceUnits {
		sources = append(sources, source)
	}
	sortBudgetSources(sources)
	return sources
}

func sortBudgetSources(sources []poller.BudgetSource) {
	for i := 1; i < len(sources); i++ {
		current := sources[i]
		j := i - 1
		for j >= 0 && string(sources[j]) > string(current) {
			sources[j+1] = sources[j]
			j--
		}
		sources[j+1] = current
	}
}

func (l *globalBudgetLimiter) newOwnerToken(job poller.BudgetJob) (string, error) {
	var randomBytes [16]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", err
	}
	material := strings.Join([]string{
		l.instanceID,
		job.Namespace,
		job.InstanceID,
		job.PollerName,
		job.ChannelID,
		job.JobKey,
	}, "\x00")
	sum := sha256.Sum256([]byte(material))
	return sanitizeGlobalBudgetTokenPart(l.instanceID) + ":" + hex.EncodeToString(sum[:8]) + ":" + hex.EncodeToString(randomBytes[:]), nil
}

func sanitizeGlobalBudgetTokenPart(value string) string {
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}

func normalizeGlobalBudgetNamespace(namespace string) string {
	normalized := strings.TrimSpace(namespace)
	if normalized == "" {
		return ""
	}
	normalized = strings.ToLower(normalized)
	normalized = strings.ReplaceAll(normalized, ":", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	return normalized
}

func normalizeGlobalBudgetInstanceID(instanceID string) string {
	normalized := strings.TrimSpace(instanceID)
	if normalized == "" {
		return "unknown"
	}
	return normalized
}

func copySourceMaxInflight(source map[poller.BudgetSource]int) map[poller.BudgetSource]int {
	copied := make(map[poller.BudgetSource]int, len(source))
	for key, value := range source {
		copied[key] = value
	}
	return copied
}

func copyClassMaxInflight(class map[poller.BudgetBurstClass]int) map[poller.BudgetBurstClass]int {
	copied := make(map[poller.BudgetBurstClass]int, len(class))
	for key, value := range class {
		copied[key] = value
	}
	return copied
}

func durationMillis(ttl time.Duration) int64 {
	ms := ttl.Milliseconds()
	if ms <= 0 {
		return 1
	}
	return ms
}

func millisDuration(ms int64) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}
