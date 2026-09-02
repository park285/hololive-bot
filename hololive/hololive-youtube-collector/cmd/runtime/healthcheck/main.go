package main

import (
	"fmt"
	"os"
	"time"
	_ "time/tzdata"

	"github.com/park285/shared-go/v2/pkg/healthprobe"
)

const externalSmokeURL = "https://www.google.com/generate_204"

// --smoke는 collector 이미지 계약이라 여기서 소유한다: 시간대 데이터와 함께 내부 CA override를 걷어낸
// 시스템 CA 번들로 외부 HTTPS를 검증한다. 나머지 모드는 shared-go healthprobe CLI를 그대로 쓴다.
func main() {
	if len(os.Args) == 2 && os.Args[1] == "--smoke" {
		os.Exit(runSmoke())
	}

	os.Exit(healthprobe.RunMain(os.Args, os.Stdout, os.Stderr))
}

func runSmoke() int {
	for _, name := range []string{"Asia/Seoul", "Asia/Tokyo", "UTC"} {
		if _, err := time.LoadLocation(name); err != nil {
			fmt.Fprintf(os.Stderr, "load location %s: %v\n", name, err)

			return 1
		}
	}

	if err := clearInternalTLSOverrides(); err != nil {
		fmt.Fprintf(os.Stderr, "https ca smoke: %v\n", err)

		return 1
	}

	if err := healthprobe.CheckURL(externalSmokeURL); err != nil {
		fmt.Fprintf(os.Stderr, "https ca smoke: %v\n", err)

		return 1
	}

	if _, err := fmt.Fprintln(os.Stdout, "smoke ok"); err != nil {
		fmt.Fprintln(os.Stderr, err)

		return 1
	}

	return 0
}

func clearInternalTLSOverrides() error {
	if err := os.Unsetenv(healthprobe.CACertFileEnv); err != nil {
		return fmt.Errorf("clear internal ca override: %w", err)
	}

	if err := os.Unsetenv(healthprobe.ServerNameEnv); err != nil {
		return fmt.Errorf("clear internal server name override: %w", err)
	}

	return nil
}
