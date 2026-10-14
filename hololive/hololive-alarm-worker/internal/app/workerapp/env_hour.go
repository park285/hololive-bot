package workerapp

import (
	"fmt"

	"github.com/park285/shared-go/v2/pkg/envutil"
)

// parseHourOfDayEnv는 key의 값을 0부터 23까지의 시(hour)로 읽는다. 값이 없으면 def를 쓰고, 범위 밖이면 기본값으로
// 바꾸지 않고 설정 오류를 돌려준다. 오류에는 값을 넣지 않는다.
func parseHourOfDayEnv(key string, def int) (int, error) {
	value, err := envutil.IntE(key, def)
	if err != nil {
		return 0, fmt.Errorf("read hour env: %w", err)
	}

	if value < 0 || value > 23 {
		return 0, fmt.Errorf("%s must be an hour between 0 and 23", key)
	}

	return value, nil
}
