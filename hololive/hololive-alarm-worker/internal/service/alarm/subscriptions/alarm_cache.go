package subscriptions

import (
	"time"
)

// cacheCleanupTimeout은 DB 반영 뒤 요청 취소와 분리해 cache를 맞추는 작업의 상한이다.
const cacheCleanupTimeout = 5 * time.Second
