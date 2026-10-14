package leaseowner

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// InstanceID는 lease owner를 "prefix:hostname:pid"로 만든다. 호스트 이름 조회 실패는 오류다.
// 여러 호스트가 같은 ID를 사용하면 claim 소유 판정이 어긋난다.
func InstanceID(prefix string) (string, error) {
	return instanceIDWithHostname(prefix, os.Hostname)
}

func instanceIDWithHostname(prefix string, hostname func() (string, error)) (string, error) {
	host, err := hostname()
	if err != nil {
		return "", fmt.Errorf("instance id: hostname: %w", err)
	}

	if strings.TrimSpace(host) == "" {
		return "", errors.New("instance id: hostname is empty")
	}

	return fmt.Sprintf("%s:%s:%d", prefix, host, os.Getpid()), nil
}
