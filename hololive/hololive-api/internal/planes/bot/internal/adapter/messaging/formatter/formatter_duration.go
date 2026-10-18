package formatter

import (
	"fmt"
	"strings"
	"time"
)

// formatBroadcastDuration은 방송 길이와 경과 시간을 분 단위로 내림해 "3일 5시간 12분"처럼 0인 단위를 빼고 표기한다.
// 1분 미만과 음수는 "1분 미만"이다. 내림은 진행 중 방송의 경과 표기와 같은 기준이며, 표기한 길이가 실제보다
// 길어지지 않게 한다.
func formatBroadcastDuration(d time.Duration) string {
	minutes := int64(d / time.Minute)
	if minutes < 1 {
		return "1분 미만"
	}

	days, rem := minutes/(24*60), minutes%(24*60)
	hours, mins := rem/60, rem%60

	parts := make([]string, 0, 3)

	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d일", days))
	}

	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d시간", hours))
	}

	if mins > 0 {
		parts = append(parts, fmt.Sprintf("%d분", mins))
	}

	return strings.Join(parts, " ")
}
