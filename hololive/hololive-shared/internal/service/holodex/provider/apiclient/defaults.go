package apiclient

import "time"

// 재시도 백오프 기준값입니다. 시도 횟수 상한은 설정의 MaxRetryAttempts가 소유합니다.
const (
	retryBaseDelay = 500 * time.Millisecond
	retryJitter    = 250 * time.Millisecond
)

// 연속 실패가 circuitFailureThreshold에 이르면 circuit을 열고, circuitResetTimeout 뒤 다시 요청을 허용합니다.
// 실패 요청 재시도 지연(holodexprovider)이 이 resetTimeout보다 길게 잡혀 있으므로 함께 바꿔야 합니다.
const (
	circuitFailureThreshold = 3
	circuitResetTimeout     = 30 * time.Second
)
