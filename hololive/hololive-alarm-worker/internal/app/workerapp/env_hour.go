package workerapp

import (
	"fmt"
	"os"
	"strings"

	"github.com/park285/shared-go/v2/pkg/envutil"
)

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
