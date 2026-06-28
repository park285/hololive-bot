package util

import (
	"fmt"
	"os"
)

func InstanceID(prefix string) string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s:%s:%d", prefix, hostname, os.Getpid())
}
