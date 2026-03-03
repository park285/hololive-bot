package majorevent

import "errors"

// ErrNotificationInProgress: 동일 알림이 이미 실행 중인 경우 반환합니다.
//
// NOTE: scheduler 구현이 서브패키지로 이동하더라도(Phase9 P9-5),
// 트리거 핸들러/클라이언트가 에러를 안정적으로 매핑할 수 있도록 root에 유지합니다.
var ErrNotificationInProgress = errors.New("notification already in progress")
