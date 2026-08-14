package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/contracts/common"
	"github.com/park285/shared-go/pkg/healthprobe"
)

func main() {
	args := os.Args[1:]
	if len(args) == 2 && args[0] == "--body" {
		runBody(args[1])
		return
	}
	if len(args) == 3 && args[0] == "--body-api-key-env" {
		runBodyWithAPIKeyEnv(args[1], args[2])
		return
	}

	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: healthcheck <url>|--body <url>|--body-api-key-env <env> <url>|--smoke")
		os.Exit(2)
	}

	if args[0] == "--smoke" {
		runSmoke()
		return
	}

	runCheck(args[0])
}

func runBody(url string) {
	body, err := healthprobe.FetchURLInternal(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if _, err := os.Stdout.Write(body); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runBodyWithAPIKeyEnv(envName, url string) {
	body, err := fetchBodyWithAPIKeyEnv(envName, url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if _, err := os.Stdout.Write(body); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func fetchBodyWithAPIKeyEnv(envName, url string) ([]byte, error) {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return nil, fmt.Errorf("api key env name must not be empty")
	}
	apiKey := os.Getenv(envName)
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("%s is empty or not set", envName)
	}
	return healthprobe.FetchURLWithHeadersInternal(url, map[string]string{common.APIKeyHeader: apiKey})
}

func runCheck(url string) {
	if err := healthprobe.CheckURLInternal(url); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runSmoke() {
	for _, name := range []string{"Asia/Seoul", "Asia/Tokyo", "UTC"} {
		if _, err := time.LoadLocation(name); err != nil {
			fmt.Fprintf(os.Stderr, "load location %s: %v\n", name, err)
			os.Exit(1)
		}
	}

	if err := healthprobe.CheckURL("https://www.google.com"); err != nil {
		fmt.Fprintf(os.Stderr, "https ca smoke: %v\n", err)
		os.Exit(1)
	}

	if _, err := fmt.Fprintln(os.Stdout, "smoke ok"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
