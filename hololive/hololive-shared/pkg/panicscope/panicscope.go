// Package panicscope는 hololive 서비스가 shared-go panicguard에 넘기는 로그 계약을 한 곳에 둔다.
package panicscope

import "github.com/park285/shared-go/v2/pkg/panicguard"

// BackgroundTask는 백그라운드 goroutine panic 복구 로그 계약이다.
// 공개 var는 shared-go v2.18.0에서 삭제됐으므로 기존 로그 문구와 키를 그대로 유지하려고 여기서 정의한다.
var BackgroundTask = panicguard.LogContract{Message: "background goroutine panic recovered", NameKey: "guard"}
