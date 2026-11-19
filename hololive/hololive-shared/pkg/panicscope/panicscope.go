package panicscope

import "github.com/park285/shared-go/v2/pkg/panicguard"

var BackgroundTask = panicguard.LogContract{Message: "background goroutine panic recovered", NameKey: "guard"}
