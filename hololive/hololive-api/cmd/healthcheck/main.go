package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/kapu/hololive-shared/pkg/contracts/common"
	"github.com/park285/shared-go/pkg/healthprobe"
)

func main() {
	args := os.Args[1:]
	if len(args) >= 3 && args[0] == "--api-key-env" {
		runChecksWithAPIKeyEnv(args[1], args[2:])
		return
	}

	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: healthcheck <url> [url...]|--api-key-env <env> <url> [url...]")
		os.Exit(2)
	}
	for _, target := range args {
		if err := healthprobe.CheckURLInternal(target); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

func runChecksWithAPIKeyEnv(envName string, targets []string) {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		fmt.Fprintln(os.Stderr, "api key env name must not be empty")
		os.Exit(2)
	}
	apiKey := os.Getenv(envName)
	if strings.TrimSpace(apiKey) == "" {
		fmt.Fprintf(os.Stderr, "%s is empty or not set\n", envName)
		os.Exit(1)
	}

	headers := map[string]string{common.APIKeyHeader: apiKey}
	for _, target := range targets {
		if _, err := healthprobe.FetchURLWithHeadersInternal(target, headers); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
