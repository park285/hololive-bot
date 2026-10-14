package workerapp

import (
	"fmt"
	"os"
	"strings"

	"github.com/park285/shared-go/v2/pkg/envutil"
)

// parseHourOfDayEnv는 key의 값을 0부터 23까지의 시(hour)로 읽는다. 키가 없으면 def를 쓴다.
// 키가 있는데 비어 있거나 공백뿐이면, 또는 정수가 아니거나 범위 밖이면 기본값으로 바꾸지 않고 설정 오류를 돌려준다.
// 빈 값 규칙은 envload.RequiredPositiveIntEnv와 같고, 오류에는 값을 넣지 않는다.
func parseHourOfDayEnv(key string, def int) (int, error) {
	raw, found := os.LookupEnv(key)
	if !found {
		return def, nil
	}

	if strings.TrimSpace(raw) == "" {
		return 0, fmt.Errorf("%s must be an hour between 0 and 23", key)
	}

	value, err := envutil.IntE(key, def)
	if err != nil {
		return 0, fmt.Errorf("read hour env: %w", err)
	}

	if value < 0 || value > 23 {
		return 0, fmt.Errorf("%s must be an hour between 0 and 23", key)
	}

	return value, nil
}
