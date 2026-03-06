package cache

import "fmt"

// CacheError는 cache(Service) 계층에서 발생한 오류를 구조화하여 전달합니다.
//
// NOTE: 기존 hololive-shared/pkg/errors.CacheError 의존을 제거하기 위한 로컬 타입입니다.
type CacheError struct {
	Operation string // get, set, delete 등
	Key       string // cache key (선택)
	Err       error  // 원인 에러
}

func (e *CacheError) Error() string {
	if e.Err == nil {
		if e.Key == "" {
			return fmt.Sprintf("cache: %s", e.Operation)
		}
		return fmt.Sprintf("cache: %s: key=%s", e.Operation, e.Key)
	}

	if e.Key == "" {
		return fmt.Sprintf("cache: %s: %v", e.Operation, e.Err)
	}
	return fmt.Sprintf("cache: %s: key=%s: %v", e.Operation, e.Key, e.Err)
}

func (e *CacheError) Unwrap() error { return e.Err }

// NewCacheError는 cache 에러를 생성합니다.
//
// message 파라미터는 레거시 호환을 위해 유지합니다(현재 Error() 문자열에는 포함하지 않음).
func NewCacheError(_ string, operation, key string, cause error) *CacheError {
	return &CacheError{
		Operation: operation,
		Key:       key,
		Err:       cause,
	}
}
