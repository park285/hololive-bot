package constants

import "time"

var ValkeyTimeout = struct {
	ConnWriteTimeout time.Duration
	DialTimeout      time.Duration
}{
	ConnWriteTimeout: 3 * time.Second,
	DialTimeout:      5 * time.Second,
}
