package collection

import (
	"errors"
	"time"
)

// 수집 결과 한 건이 담을 수 있는 관측 수와 수집 지연의 상한입니다. 결과 생성·검증·발행이 같은 값을 씁니다.
const (
	MaxPublishBatchSize  = 1024
	MaxCollectionLatency = 24 * time.Hour
)

// lease 결과 sentinel은 lease 저장소와 발행 저장소가 함께 반환하므로 두 adapter 어느 쪽도 소유하지 않습니다.
var (
	ErrFenceLost       = errors.New("collection job fence was lost")
	ErrProjectionStale = errors.New("collection projection is stale")
	ErrTargetDisabled  = errors.New("collection target is disabled")
)
