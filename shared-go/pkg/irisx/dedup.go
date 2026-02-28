package irisx

import (
	"fmt"
	"strings"
)

// DedupKey: webhook 메시지 ID 기반 dedup 키를 생성합니다.
// messageID가 비어 있으면 빈 문자열을 반환합니다.
func DedupKey(messageID string) string {
	id := strings.TrimSpace(messageID)
	if id == "" {
		return ""
	}
	// hash-tag({})를 사용해 cluster 환경에서도 같은 slot에 배치되도록 고정합니다.
	return fmt.Sprintf("iris:msg:{%s}", id)
}
