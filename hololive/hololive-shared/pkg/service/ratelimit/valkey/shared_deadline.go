package valkey

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	valkeygo "github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

// extendDeadlineScript는 저장된 시각보다 늦을 때만 값을 바꾼다. 여러 프로세스가 동시에 기록해도 대기 시각은 뒤로만 밀린다.
// 본문은 hololive-api·hololive-worker ACL이 허용하는 GET과 SET만 쓴다.
const extendDeadlineScript = `
local current = tonumber(redis.call('GET', KEYS[1]) or '') or 0
local candidate = tonumber(ARGV[1])
if candidate <= current then
  return 0
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
return 1
`

var extendDeadlineLua = valkeygo.NewLuaScript(extendDeadlineScript)

// SharedDeadline은 여러 프로세스가 함께 지키는 대기 시각(429 cooldown, circuit open)을 Valkey에 둔다.
// 값은 Unix ms 시각이고 key는 그 시각에 만료한다. 만료·축출돼도 되는 rate limit 상태이며 정본이 아니다.
type SharedDeadline struct {
	cacheClient cache.LowLevelCache
	now         func() time.Time
}

func NewSharedDeadline(cacheClient cache.LowLevelCache) (*SharedDeadline, error) {
	if cacheClient == nil {
		return nil, errors.New("new shared deadline: cache service must not be nil")
	}

	return &SharedDeadline{cacheClient: cacheClient, now: time.Now}, nil
}

// Extend는 key의 대기 시각을 until로 늦춘다. 이미 더 늦은 시각이 있으면 그대로 둔다.
func (d *SharedDeadline) Extend(ctx context.Context, key string, until time.Time) error {
	ttl, ok := d.remaining(until)
	if !ok {
		return nil
	}

	result := extendDeadlineLua.Exec(ctx, d.cacheClient.GetClient(), []string{key}, []string{
		strconv.FormatInt(until.UnixMilli(), 10),
		strconv.FormatInt(ttl.Milliseconds(), 10),
	})
	if err := result.Error(); err != nil {
		return fmt.Errorf("extend shared deadline: %w", err)
	}

	return nil
}

// OpenOnce는 key가 비어 있을 때만 until까지 연다. 이미 열려 있으면 false를 돌려주고 만료 시각을 늘리지 않는다.
func (d *SharedDeadline) OpenOnce(ctx context.Context, key string, until time.Time) (bool, error) {
	ttl, ok := d.remaining(until)
	if !ok {
		return false, nil
	}

	cmd := d.cacheClient.B().Set().Key(key).Value(strconv.FormatInt(until.UnixMilli(), 10)).Nx().Px(ttl).Build()

	err := d.cacheClient.GetClient().Do(ctx, cmd).Error()
	if valkeygo.IsValkeyNil(err) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("open shared deadline: %w", err)
	}

	return true, nil
}

// Deadline은 key의 대기 시각을 읽는다. 값이 없으면 zero time을, 숫자가 아닌 값이면 오류를 돌려준다.
func (d *SharedDeadline) Deadline(ctx context.Context, key string) (time.Time, error) {
	raw, err := d.cacheClient.GetClient().Do(ctx, d.cacheClient.B().Get().Key(key).Build()).ToString()
	if valkeygo.IsValkeyNil(err) {
		return time.Time{}, nil
	}

	if err != nil {
		return time.Time{}, fmt.Errorf("read shared deadline: %w", err)
	}

	unixMilli, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse shared deadline %q: %w", raw, err)
	}

	return time.UnixMilli(unixMilli), nil
}

// remaining은 until까지 남은 시간을 1ms 이상으로 올린다. 이미 지난 시각은 기록할 것이 없다.
func (d *SharedDeadline) remaining(until time.Time) (time.Duration, bool) {
	ttl := until.Sub(d.now())
	if ttl <= 0 {
		return 0, false
	}

	return max(ttl.Round(time.Millisecond), time.Millisecond), true
}
