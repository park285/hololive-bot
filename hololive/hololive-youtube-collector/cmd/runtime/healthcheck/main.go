package main

import (
	"context"
	"fmt"
	"io"
	"os"
	_ "time/tzdata"

	"github.com/park285/shared-go/v2/pkg/healthprobe"
)

const externalSmokeURL = "https://www.google.com/generate_204"

// --smoke는 collector 이미지 계약이라 여기서 소유한다: shared-go smoke(시간대 데이터)에 더해
// 시스템 CA 번들로 외부 HTTPS를 검증한다. CheckURL은 HEALTHCHECK_* 내부 TLS override를 읽지 않는다.
// 나머지 모드는 shared-go healthprobe CLI를 그대로 쓴다.
func main() {
	if len(os.Args) == 2 && os.Args[1] == "--smoke" {
		os.Exit(runSmoke(os.Args, os.Stdout, os.Stderr))
	}

	os.Exit(healthprobe.RunMain(os.Args, os.Stdout, os.Stderr))
}

func runSmoke(args []string, stdout, stderr io.Writer) int {
	// 시간대 목록은 shared-go smoke가 정본이므로 그 결과 출력("smoke ok")만 버리고 종료 코드를 따른다.
	if code := healthprobe.RunMain(args, io.Discard, stderr); code != 0 {
		return code
	}

	// 요청 기한은 healthprobe 내부 요청 timeout이 소유한다.
	if err := healthprobe.CheckURL(context.Background(), externalSmokeURL); err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "https ca smoke: %v\n", err); writeErr != nil {
			return 1
		}

		return 1
	}

	if _, err := fmt.Fprintln(stdout, "smoke ok"); err != nil {
		return 1
	}

	return 0
}
