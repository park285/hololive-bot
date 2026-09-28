package load

import (
	"errors"
	"time"

	sharedenv "github.com/park285/shared-go/v2/pkg/envutil"
)

// StrictEnv는 설정 로더 하나가 읽는 숫자·bool env를 shared-go 엄격 파서(*E)로 읽고 파싱 오류를 모은다.
// 비엄격 파서처럼 잘못된 값을 경고 뒤 기본값으로 바꿔 기동을 이어 가지 않는다
// (stack audit B4, PLN-20260926-stack-audit-refactoring T10). 값이 없거나 공백뿐이면 기본값이다.
//
// 오류가 난 키의 반환값은 의미가 없다. 로더는 Err()가 nil이 아니면 만든 설정을 버리고 그 오류를 돌려준다.
// 이 로더가 읽는 잘못된 키를 한 번의 기동 실패로 모두 보이도록 첫 오류에서 멈추지 않는다. 다른 로더의 오류와 합칠지는
// 호출자가 정한다. 오류 문구는 키 이름만 담고 값은 담지 않는다.
type StrictEnv struct {
	errs []error
}

func (e *StrictEnv) Int(key string, def int) int {
	return e.keep(sharedenv.IntE(key, def))
}

func (e *StrictEnv) Int64(key string, def int64) int64 {
	return e.keep(sharedenv.Int64E(key, def))
}

func (e *StrictEnv) Bool(key string, def bool) bool {
	return e.keep(sharedenv.BoolE(key, def))
}

func (e *StrictEnv) Float(key string, def float64) float64 {
	return e.keep(sharedenv.FloatE(key, def))
}

// Seconds는 정수 초 단위 env를 읽는다. 기본값은 초 미만을 버린 값이고, time.Duration 범위를 넘으면 오류다.
func (e *StrictEnv) Seconds(key string, def time.Duration) time.Duration {
	return e.keep(StrictDurationUnitEnv(key, def, time.Second))
}

// Millis는 정수 밀리초 단위 env를 읽는다. 기본값은 밀리초 미만을 버린 값이고, time.Duration 범위를 넘으면 오류다.
func (e *StrictEnv) Millis(key string, def time.Duration) time.Duration {
	return e.keep(StrictDurationUnitEnv(key, def, time.Millisecond))
}

// Err는 지금까지 모은 파싱 오류를 합쳐 돌려준다. 오류가 없으면 nil이다.
func (e *StrictEnv) Err() error {
	return errors.Join(e.errs...)
}

func (e *StrictEnv) keep[T any](value T, err error) T {
	if err != nil {
		e.errs = append(e.errs, err)
	}

	return value
}
