package envconfig

import (
	"fmt"
	"math"
	"time"

	"github.com/park285/shared-go/v2/pkg/envutil"
)

// 값이 없거나 공백뿐이면 def를 쓴다. 값이 있으면 정수여야 하고 각 함수의 범위를 지켜야 하며, 어긋나면 기본값으로
// 바꾸지 않고 설정 오류를 돌려준다(stack audit holo-alarm-worker-envconfig-silent-defaults). 오류에는 값을 넣지 않는다.

// ParsePositiveInt는 key의 값을 양의 정수로 읽는다.
func ParsePositiveInt(key string, def int) (int, error) {
	value, err := envutil.IntE(key, def)
	if err != nil {
		return 0, fmt.Errorf("read positive int env: %w", err)
	}

	if value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}

	return value, nil
}

// ParseHourOfDay는 key의 값을 0부터 23까지의 시(hour)로 읽는다.
func ParseHourOfDay(key string, def int) (int, error) {
	value, err := envutil.IntE(key, def)
	if err != nil {
		return 0, fmt.Errorf("read hour env: %w", err)
	}

	if value < 0 || value > 23 {
		return 0, fmt.Errorf("%s must be an hour between 0 and 23", key)
	}

	return value, nil
}

// ParsePositiveDurationMS는 key의 값을 양의 밀리초로 읽고, time.Duration 범위를 넘으면 거절한다.
func ParsePositiveDurationMS(key string, def time.Duration) (time.Duration, error) {
	value, err := envutil.Int64E(key, def.Milliseconds())
	if err != nil {
		return 0, fmt.Errorf("read positive millisecond env: %w", err)
	}

	if value <= 0 || value > math.MaxInt64/int64(time.Millisecond) {
		return 0, fmt.Errorf("%s must be a positive millisecond count within the duration range", key)
	}

	return time.Duration(value) * time.Millisecond, nil
}
