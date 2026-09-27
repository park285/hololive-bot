package util

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// InstanceID는 lease owner 등 worker 식별자를 "prefix:hostname:pid"로 만든다. 호스트 이름을 얻지 못하면
// "unknown-host"로 바꾸지 않고 오류다(DEC-20260926-hololive-legacy-env-config-retirement). 여러 host의 worker가 같은
// 식별자를 쓰면 claim 소유 판정이 어긋나기 때문이다.
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
