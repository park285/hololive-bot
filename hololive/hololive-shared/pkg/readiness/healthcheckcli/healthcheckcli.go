package healthcheckcli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/park285/shared-go/pkg/healthprobe"

	"github.com/kapu/hololive-shared/pkg/contracts/common"
)

func Run(args []string, stderr io.Writer) int {
	if len(args) >= 3 && args[0] == "--api-key-env" {
		return runChecksWithAPIKeyEnv(stderr, args[1], args[2:])
	}

	if len(args) < 1 {
		if _, err := fmt.Fprintln(stderr, "usage: healthcheck <url> [url...]|--api-key-env <env> <url> [url...]"); err != nil {
			return 1
		}
		return 2
	}
	for _, target := range args {
		if err := healthprobe.CheckURLInternal(target); err != nil {
			return reportFailure(stderr, err)
		}
	}
	return 0
}

func runChecksWithAPIKeyEnv(stderr io.Writer, envName string, targets []string) int {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		if _, err := fmt.Fprintln(stderr, "api key env name must not be empty"); err != nil {
			return 1
		}
		return 2
	}
	apiKey := os.Getenv(envName)
	if strings.TrimSpace(apiKey) == "" {
		return reportFailure(stderr, fmt.Errorf("%s is empty or not set", envName))
	}

	headers := map[string]string{common.APIKeyHeader: apiKey}
	for _, target := range targets {
		if _, err := healthprobe.FetchURLWithHeadersInternal(target, headers); err != nil {
			return reportFailure(stderr, err)
		}
	}
	return 0
}

func reportFailure(stderr io.Writer, failure error) int {
	_, err := fmt.Fprintln(stderr, failure)
	if err != nil {
		return 1
	}
	return 1
}
