package instanceid

import (
	"os"
	"strings"
)

// 고정 문자열로 폴백하면 INSTANCE_ID 누락 시 여러 인스턴스의 lease/budget
// 토큰 이름공간이 하나로 겹치므로 hostname을 우선한다.
func Normalize(instanceID string) string {
	normalized := strings.TrimSpace(instanceID)
	if normalized != "" {
		return normalized
	}
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		return strings.TrimSpace(hostname)
	}
	return "unknown"
}
