//go:build linux

package youtubejs

import (
	"fmt"
	"os"
	"syscall"
)

func requireHelperPlatform() error {
	return nil
}

func verifyHelperSocket(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("start youtube.js helper: inspect socket: %w", err)
	}
	if info.Mode().Type() != os.ModeSocket {
		return fmt.Errorf("start youtube.js helper: path is not a unix socket")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("start youtube.js helper: socket permits group or other access")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("start youtube.js helper: socket owner is unavailable")
	}
	euid := os.Geteuid()
	if euid < 0 || uint64(stat.Uid) != uint64(euid) {
		return fmt.Errorf("start youtube.js helper: socket owner mismatch")
	}
	return nil
}
