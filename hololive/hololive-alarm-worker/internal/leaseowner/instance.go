package leaseowner

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

func InstanceID(prefix string) (string, error) {
	return instanceIDWithHostname(prefix, os.Hostname)
}

func instanceIDWithHostname(prefix string, hostname func() (string, error)) (string, error) {
	host, err := hostname()
	if err != nil {
		return "", fmt.Errorf("instance id: hostname: %w", err)
	}

	if strings.TrimSpace(host) == "" {
		return "", errors.New("instance id: hostname is empty")
	}

	return fmt.Sprintf("%s:%s:%d", prefix, host, os.Getpid()), nil
}
