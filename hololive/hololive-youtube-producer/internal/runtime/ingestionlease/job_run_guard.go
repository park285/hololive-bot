package ingestionlease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/valkey-io/valkey-go"
)

type JobIdentity struct {
	PollerName string
	ChannelID  string
	Interval   time.Duration
}

type JobClaimResult string

const (
	JobClaimAcquired         JobClaimResult = "acquired"
	JobClaimPeerOwned        JobClaimResult = "peer_owned"
	JobClaimAlreadyCompleted JobClaimResult = "already_completed"
	JobClaimUnavailable      JobClaimResult = "unavailable"
)

type JobClaimStatus struct {
	Result     JobClaimResult
	RetryAfter time.Duration
	LeaseTTL   time.Duration
	OwnerToken string
}

type JobRunGuardConfig struct {
	Namespace  string
	InstanceID string
}

type JobRunGuard struct {
	cacheClient cache.Client
	namespace   string
	instanceID  string
}

type JobLeaseKeys struct {
	LeaseKey    string
	CooldownKey string
}

type JobRunClaim struct {
	cacheClient cache.Client
	keys        JobLeaseKeys
	ownerToken  string
}

const (
	defaultJobRunGuardNamespace = "production"

	jobRunGuardAcquireCodePeerOwned        int64 = 0
	jobRunGuardAcquireCodeAcquired         int64 = 1
	jobRunGuardAcquireCodeAlreadyCompleted int64 = 2
)

const acquireJobRunScript = `
local cooldownKey = KEYS[1]
local leaseKey = KEYS[2]
local owner = ARGV[1]
local leaseTTL = tonumber(ARGV[2])
if redis.call('EXISTS', cooldownKey) == 1 then
  local ttl = redis.call('PTTL', cooldownKey)
  if ttl < 0 then ttl = 0 end
  return {2, ttl}
end
if redis.call('SET', leaseKey, owner, 'NX', 'PX', leaseTTL) then
  return {1, leaseTTL}
end
local ttl = redis.call('PTTL', leaseKey)
if ttl < 0 then ttl = leaseTTL end
return {0, ttl}
`

const renewJobRunScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 0
`

const completeJobRunScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  redis.call('SET', KEYS[2], ARGV[1], 'PX', ARGV[2])
  redis.call('DEL', KEYS[1])
  return 1
end
return 0
`

const releaseJobRunScript = `
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`

func NewJobRunGuard(cacheClient cache.Client, config JobRunGuardConfig) *JobRunGuard {
	return &JobRunGuard{
		cacheClient: cacheClient,
		namespace:   normalizeJobRunGuardNamespace(config.Namespace),
		instanceID:  normalizeJobRunGuardInstanceID(config.InstanceID),
	}
}

func BuildJobLeaseKeys(namespace string, identity JobIdentity) (JobLeaseKeys, error) {
	pollerName := strings.TrimSpace(identity.PollerName)
	channelID := strings.TrimSpace(identity.ChannelID)
	if pollerName == "" {
		return JobLeaseKeys{}, fmt.Errorf("build job lease keys: poller name must not be empty")
	}
	if channelID == "" {
		return JobLeaseKeys{}, fmt.Errorf("build job lease keys: channel id must not be empty")
	}

	ns := normalizeJobRunGuardNamespace(namespace)
	tag := fmt.Sprintf("{job:%s:%s}", shortHash(pollerName), shortHash(channelID))
	prefix := fmt.Sprintf("hololive:%s:youtube-producer:%s", ns, tag)
	return JobLeaseKeys{
		LeaseKey:    prefix + ":lease",
		CooldownKey: prefix + ":cooldown",
	}, nil
}

func (g *JobRunGuard) TryClaim(
	ctx context.Context,
	identity JobIdentity,
	leaseTTL time.Duration,
	cooldownTTL time.Duration,
) (JobClaimStatus, *JobRunClaim, error) {
	if g == nil || g.cacheClient == nil {
		return JobClaimStatus{Result: JobClaimUnavailable}, nil, fmt.Errorf("try claim job run: cache service must not be nil")
	}
	keys, err := BuildJobLeaseKeys(g.namespace, identity)
	if err != nil {
		return JobClaimStatus{Result: JobClaimUnavailable}, nil, err
	}
	if err := validateJobRunTTLs(leaseTTL, cooldownTTL); err != nil {
		return JobClaimStatus{Result: JobClaimUnavailable}, nil, err
	}

	ownerToken := g.newOwnerToken()
	result, retryAfter, err := g.acquire(ctx, keys, ownerToken, leaseTTL)
	status := JobClaimStatus{
		Result:     result,
		RetryAfter: retryAfter,
		LeaseTTL:   leaseTTL,
	}
	if err != nil {
		status.Result = JobClaimUnavailable
		return status, nil, err
	}
	if result != JobClaimAcquired {
		return status, nil, nil
	}
	status.OwnerToken = ownerToken
	return status, &JobRunClaim{
		cacheClient: g.cacheClient,
		keys:        keys,
		ownerToken:  ownerToken,
	}, nil
}

func (g *JobRunGuard) acquire(
	ctx context.Context,
	keys JobLeaseKeys,
	ownerToken string,
	leaseTTL time.Duration,
) (JobClaimResult, time.Duration, error) {
	cmd := g.cacheClient.B().
		Eval().
		Script(acquireJobRunScript).
		Numkeys(2).
		Key(keys.CooldownKey, keys.LeaseKey).
		Arg(ownerToken, strconv.FormatInt(durationMillis(leaseTTL), 10)).
		Build()
	values, err := evalArray(ctx, g.cacheClient, cmd)
	if err != nil {
		return JobClaimUnavailable, 0, fmt.Errorf("acquire job run: %w", err)
	}
	return parseAcquireJobRunResult(values)
}

func parseAcquireJobRunResult(values []valkey.ValkeyMessage) (JobClaimResult, time.Duration, error) {
	code, retryAfterMS, err := parseAcquireJobRunValues(values)
	if err != nil {
		return JobClaimUnavailable, 0, err
	}
	switch code {
	case jobRunGuardAcquireCodeAcquired:
		return JobClaimAcquired, 0, nil
	case jobRunGuardAcquireCodeAlreadyCompleted:
		return JobClaimAlreadyCompleted, millisDuration(retryAfterMS), nil
	case jobRunGuardAcquireCodePeerOwned:
		return JobClaimPeerOwned, millisDuration(retryAfterMS), nil
	default:
		return JobClaimUnavailable, 0, fmt.Errorf("acquire job run: unknown result code: %d", code)
	}
}

func parseAcquireJobRunValues(values []valkey.ValkeyMessage) (code, retryAfterMS int64, err error) {
	if len(values) != 2 {
		return 0, 0, fmt.Errorf("acquire job run: unexpected result length: %d", len(values))
	}
	code, err = values[0].AsInt64()
	if err != nil {
		return 0, 0, fmt.Errorf("acquire job run: parse result code: %w", err)
	}
	retryAfterMS, err = values[1].AsInt64()
	if err != nil {
		return 0, 0, fmt.Errorf("acquire job run: parse retry ttl: %w", err)
	}
	return code, retryAfterMS, nil
}

func (c *JobRunClaim) Renew(ctx context.Context, ttl time.Duration) (bool, error) {
	if c == nil || c.cacheClient == nil {
		return false, fmt.Errorf("renew job run: claim must not be nil")
	}
	if ttl <= 0 {
		return false, fmt.Errorf("renew job run: ttl must be positive")
	}
	cmd := c.cacheClient.B().
		Eval().
		Script(renewJobRunScript).
		Numkeys(1).
		Key(c.keys.LeaseKey).
		Arg(c.ownerToken, strconv.FormatInt(durationMillis(ttl), 10)).
		Build()
	return evalBool(ctx, c.cacheClient, cmd, "renew job run")
}

func (c *JobRunClaim) MarkCompleted(ctx context.Context, cooldownTTL time.Duration) (bool, error) {
	return c.setCooldownAndRelease(ctx, cooldownTTL, "complete job run", "cooldown ttl")
}

func (c *JobRunClaim) Defer(ctx context.Context, retryAfter time.Duration) (bool, error) {
	return c.setCooldownAndRelease(ctx, retryAfter, "defer job run", "retry after")
}

func (c *JobRunClaim) setCooldownAndRelease(ctx context.Context, ttl time.Duration, action, ttlName string) (bool, error) {
	if c == nil || c.cacheClient == nil {
		return false, fmt.Errorf("%s: claim must not be nil", action)
	}
	if ttl <= 0 {
		return false, fmt.Errorf("%s: %s must be positive", action, ttlName)
	}
	cmd := c.cacheClient.B().
		Eval().
		Script(completeJobRunScript).
		Numkeys(2).
		Key(c.keys.LeaseKey, c.keys.CooldownKey).
		Arg(c.ownerToken, strconv.FormatInt(durationMillis(ttl), 10)).
		Build()
	return evalBool(ctx, c.cacheClient, cmd, action)
}

func (c *JobRunClaim) Release(ctx context.Context) (bool, error) {
	if c == nil || c.cacheClient == nil {
		return false, fmt.Errorf("release job run: claim must not be nil")
	}
	cmd := c.cacheClient.B().
		Eval().
		Script(releaseJobRunScript).
		Numkeys(1).
		Key(c.keys.LeaseKey).
		Arg(c.ownerToken).
		Build()
	return evalBool(ctx, c.cacheClient, cmd, "release job run")
}

func (c *JobRunClaim) LeaseKey() string {
	if c == nil {
		return ""
	}
	return c.keys.LeaseKey
}

func (c *JobRunClaim) CooldownKey() string {
	if c == nil {
		return ""
	}
	return c.keys.CooldownKey
}

func (c *JobRunClaim) OwnerToken() string {
	if c == nil {
		return ""
	}
	return c.ownerToken
}

func evalArray(ctx context.Context, cacheClient cache.Client, cmd valkey.Completed) ([]valkey.ValkeyMessage, error) {
	results := cacheClient.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return nil, fmt.Errorf("unexpected result count: %d", len(results))
	}
	if results[0].Error() != nil {
		return nil, results[0].Error()
	}
	return results[0].ToArray()
}

func evalBool(ctx context.Context, cacheClient cache.Client, cmd valkey.Completed, action string) (bool, error) {
	results := cacheClient.DoMulti(ctx, cmd)
	if len(results) != 1 {
		return false, fmt.Errorf("%s: unexpected result count: %d", action, len(results))
	}
	if results[0].Error() != nil {
		return false, fmt.Errorf("%s: %w", action, results[0].Error())
	}
	value, err := results[0].AsInt64()
	if err != nil {
		return false, fmt.Errorf("%s: parse result: %w", action, err)
	}
	return value == 1, nil
}

func (g *JobRunGuard) newOwnerToken() string {
	return newOwnerToken(g.instanceID)
}

func newOwnerToken(prefix string) string {
	return fmt.Sprintf("%s:%d:%d", prefix, os.Getpid(), time.Now().UnixNano())
}

func validateJobRunTTLs(leaseTTL, cooldownTTL time.Duration) error {
	if leaseTTL <= 0 {
		return fmt.Errorf("job run guard: lease ttl must be positive")
	}
	if cooldownTTL <= 0 {
		return fmt.Errorf("job run guard: cooldown ttl must be positive")
	}
	return nil
}

func normalizeJobRunGuardNamespace(namespace string) string {
	normalized := strings.TrimSpace(namespace)
	if normalized == "" {
		return defaultJobRunGuardNamespace
	}
	normalized = strings.ToLower(normalized)
	normalized = strings.ReplaceAll(normalized, ":", "-")
	normalized = strings.ReplaceAll(normalized, " ", "-")
	return normalized
}

func normalizeJobRunGuardInstanceID(instanceID string) string {
	normalized := strings.TrimSpace(instanceID)
	if normalized != "" {
		return normalized
	}
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		return strings.TrimSpace(hostname)
	}
	return "unknown"
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])[:16]
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
