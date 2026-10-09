package targetprojection

import "time"

// 비공개·삭제된 예약 영상은 익명 영상 확인이 identity를 확인할 수 없어 UPCOMING으로 남은 채 같은 UNKNOWN만 반복한다.
// 시작 감지는 채널 스냅샷 positive가 맡으므로, 이런 영상의 재확인 주기만 추적 기간에 비례해 늦춘다. 단계가 거칠고
// 단조로운 이유는 cadence 변경이 target membership을 새로 시작하므로 영상당 전환을 세 번으로 제한하기 위해서다.
// 이후 positive가 추적을 지우면 다음 projection에서 기본 주기로 돌아온다. LIVE는 해소 불가 종료가 끝내므로 늦추지 않는다.
const (
	unresolvableBackoffFirstStep  = 10 * time.Minute
	unresolvableBackoffSecondStep = time.Hour
	unresolvableBackoffThirdStep  = 24 * time.Hour
)

// unresolvablePollInterval은 identity를 확인할 수 없는 기간에 따라 기본 주기의 1·5·15·30배(2분이면 2·10·30·60분)를 돌려준다.
func unresolvablePollInterval(base, unresolvedFor time.Duration) time.Duration {
	switch {
	case unresolvedFor < unresolvableBackoffFirstStep:
		return base
	case unresolvedFor < unresolvableBackoffSecondStep:
		return 5 * base
	case unresolvedFor < unresolvableBackoffThirdStep:
		return 15 * base
	default:
		return 30 * base
	}
}
