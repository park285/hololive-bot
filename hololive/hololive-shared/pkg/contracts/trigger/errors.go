package trigger

import "errors"

// ErrNotificationInProgress: 동일 알림이 이미 실행 중인 경우 반환합니다.
//
// NOTE: 여러 서비스(운영 API, llm-scheduler, kakao-bot)가 내부 trigger endpoint의 409를
// 안정적으로 매핑하기 위해 contracts에 유지합니다.
var ErrNotificationInProgress = errors.New("notification already in progress")
